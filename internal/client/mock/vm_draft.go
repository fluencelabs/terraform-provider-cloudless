package mock

import (
	"encoding/json"
	"net/http"
)

// /v3 draft lifecycle. Mirrors the real API's shape: POST creates a draft with
// defaults; per-aspect mutations refine it; provision flips it to a live VM
// that the /v2 handlers then serve. The mock materializes instantly (draft ->
// launched) so provider polls converge on the first read.

// defaultDraftClusterID is the cluster a fresh draft lands in, mirroring the
// server picking a default cluster. Provider tests then move it explicitly.
const defaultDraftClusterID = "00000000-0000-4000-8000-00000000dead"

// draftVerbPathParts is the segment count of /v3/vms/{id}/{verb} once split.
const draftVerbPathParts = 4

// draftStatus is the VM/storage status while unprovisioned; draftBootDiskGb
// is the default boot-disk size a fresh draft gets.
const (
	draftStatus     = "draft"
	draftBootDiskGb = 25
	// storagesVerb is the /storages path segment shared by /v2 and /v3.
	storagesVerb = "storages"
)

func (s *Server) wireVMDrafts() {
	s.mux.HandleFunc("/v3/vms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.notFound(w, r)
			return
		}
		s.createVMDraft(w, r)
	})
	s.mux.HandleFunc("/v3/vms/", s.handleVMDraftItem)
}

// createVMDraft serves POST /v3/vms: since 0.14.0 the whole draft arrives in
// one body and the answer is a 201 receipt naming what the server created.
func (s *Server) createVMDraft(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID       string                `json:"clusterId"`
		ConfigurationID string                `json:"configurationId"`
		Name            string                `json:"name"`
		SSHKeyIDs       []string              `json:"sshKeyIds"`
		BootDisk        *draftBootDiskBody    `json:"bootDisk"`
		DataDisks       []draftDiskRef        `json:"dataDisks"`
		Interfaces      *[]draftInterfaceBody `json:"interfaces"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageMap == nil {
		s.storageMap = map[string]*storageRecord{}
	}
	id := newID()
	rec := &vmRecord{
		ID: id, ClusterID: defaultDraftClusterID, ConfigurationID: newID(),
		Name: draftStatus, UserID: "test-user", Status: draftStatus,
		DataDisks: []string{}, SSHKeys: []string{},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if body.ClusterID != "" {
		rec.ClusterID = body.ClusterID
	}
	if body.ConfigurationID != "" {
		rec.ConfigurationID = body.ConfigurationID
	}
	if body.Name != "" {
		rec.Name = body.Name
	}
	if body.SSHKeyIDs != nil {
		rec.SSHKeys = body.SSHKeyIDs
	}

	storageIDs := s.seedDraftDisks(rec, body.BootDisk, dataDiskRefs(body.DataDisks))
	publicIPIDs := s.seedDraftInterfaces(rec, body.Interfaces)

	s.vmMap[id] = rec
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "acceptedStatus": draftStatus,
		"createdResources": map[string]any{"storageIds": storageIDs, "publicIpIds": publicIPIDs},
	})
}

// handleVMDraftItem dispatches /v3/vms/{id}[/...].
func (s *Server) handleVMDraftItem(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) < resourcePathParts {
		s.notFound(w, r)
		return
	}
	s.mu.Lock()
	rec, ok := s.vmMap[parts[2]]
	s.mu.Unlock()
	if !ok {
		s.writeError(w, "vm not found")
		return
	}
	// Interface routes serve drafts and live VMs alike, and since 0.12.0 so do
	// the live lifecycle verbs: /v3 carries restart, softreboot and terminate.
	if len(parts) >= draftVerbPathParts && parts[3] == interfacesVerb {
		s.handleVMInterfaceV3(w, r, rec, parts[4:])
		return
	}
	if len(parts) == draftVerbPathParts && r.Method == http.MethodPost && isLiveVMVerb(parts[3]) {
		s.handleVMVerb(w, r, rec, parts[3])
		return
	}
	if s.serveLiveVMRoute(w, r, rec, parts) {
		return
	}
	// Renaming works on a live VM too; only the SKU is draft-only.
	if len(parts) == resourcePathParts && r.Method == http.MethodPatch {
		s.updateVMDraft(w, r, rec)
		return
	}
	if rec.Status != draftStatus {
		s.writeJSON(w, http.StatusUnprocessableEntity,
			map[string]string{"error": "vm is not a draft", "code": "vm_not_draft"})
		return
	}
	switch {
	case len(parts) == resourcePathParts && r.Method == http.MethodDelete:
		s.deleteVMDraft(w, rec)
	case len(parts) == draftVerbPathParts && r.Method == http.MethodPost:
		s.handleVMDraftVerb(w, r, rec, parts[3])
	case len(parts) == draftVerbPathParts && r.Method == http.MethodPut && parts[3] == "ssh-keys":
		s.replaceDraftSSHKeys(w, r, rec)
	default:
		s.notFound(w, r)
	}
}

// handleVMInterfaceV3 dispatches /v3/vms/{id}/interfaces[/{iface}[/default]].
func (s *Server) handleVMInterfaceV3(w http.ResponseWriter, r *http.Request, rec *vmRecord, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		s.listVMInterfaces(w, rec)
	case len(rest) == 0 && r.Method == http.MethodPost:
		s.addVMInterface(w, r, rec)
	case len(rest) == 1 && r.Method == http.MethodDelete:
		s.removeVMInterface(w, rec, rest[0])
	case len(rest) == 1 && r.Method == http.MethodPatch:
		s.updateVMInterfaceV3(w, r, rec, rest[0])
	case len(rest) == 2 && rest[1] == "default" && r.Method == http.MethodPost:
		s.setDefaultVMInterface(w, rec, rest[0])
	default:
		s.notFound(w, r)
	}
}

// isLiveVMVerb names the lifecycle verbs /v3 gained in 0.12.0; they act on a
// live VM, not on a draft, and /v2 still answers them too.
func isLiveVMVerb(verb string) bool {
	return verb == "terminate" || verb == "restart" || verb == "softreboot"
}

func (s *Server) handleVMDraftVerb(w http.ResponseWriter, r *http.Request, rec *vmRecord, verb string) {
	switch verb {
	case "cluster":
		s.moveDraftCluster(w, r, rec)
	case "boot-disk":
		s.replaceDraftBootDisk(w, r, rec)
	case storagesVerb:
		s.attachDraftDataDisk(w, r, rec)
	case "provision":
		s.provisionVMDraft(w, rec)
	default:
		s.notFound(w, r)
	}
}

func (s *Server) updateVMDraft(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	var body struct {
		Name            *string `json:"name"`
		ConfigurationID *string `json:"configurationId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if body.ConfigurationID != nil && rec.Status != draftStatus {
		s.writeJSON(w, http.StatusConflict,
			map[string]string{"error": "configuration is immutable once provisioned", "code": "vm_not_draft"})
		return
	}
	if body.Name != nil {
		rec.Name = *body.Name
	}
	if body.ConfigurationID != nil {
		rec.ConfigurationID = *body.ConfigurationID
	}
	s.writeJSON(w, http.StatusOK, vmWire(rec))
}

func (s *Server) deleteVMDraft(w http.ResponseWriter, rec *vmRecord) {
	s.mu.Lock()
	// Draft-created sub-resources are hard-deleted with the draft.
	if st, ok := s.storageMap[rec.BootDisk]; ok && st.Status == draftStatus {
		delete(s.storageMap, rec.BootDisk)
	}
	for _, ni := range rec.Interfaces {
		if ip, ok := s.publicIPMap[ni.PublicIP]; ok && ip.Status == draftStatus {
			delete(s.publicIPMap, ni.PublicIP)
		}
	}
	delete(s.vmMap, rec.ID)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) moveDraftCluster(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	var body struct {
		ClusterID string `json:"clusterId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.ClusterID = body.ClusterID
	if st, ok := s.storageMap[rec.BootDisk]; ok && st.Status == draftStatus {
		st.ClusterID = body.ClusterID
	}
	s.writeJSON(w, http.StatusOK, vmWire(rec))
}

// replaceDraftBootDisk accepts either a bare storage-ID string (existing Ready
// disk) or {volumeGb, source, name?} — the source tagged "catalog" (imageId)
// or "http" (url); since 0.12.0 an untagged imageId is refused.
func (s *Server) replaceDraftBootDisk(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	var raw json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.storageMap[rec.BootDisk]; ok && old.Status == draftStatus {
		delete(s.storageMap, rec.BootDisk)
	}
	var existing string
	if json.Unmarshal(raw, &existing) == nil {
		st := s.adoptStorage(existing, rec.ClusterID, "BOOT")
		rec.BootDisk = existing
		s.writeJSON(w, http.StatusOK, publicStorageWire(st))
		return
	}
	var create struct {
		VolumeGb uint32 `json:"volumeGb"`
		Source   struct {
			Type    string `json:"type"`
			ImageID string `json:"imageId"`
			URL     string `json:"url"`
		} `json:"source"`
		Name *string `json:"name"`
	}
	_ = json.Unmarshal(raw, &create)
	image := create.Source.ImageID
	if create.Source.Type == "http" {
		image = create.Source.URL
	}
	name := rec.Name + "-boot"
	if create.Name != nil {
		name = *create.Name
	}
	id := newID()
	st := &storageRecord{
		ID: id, ClusterID: rec.ClusterID, Name: name, StorageType: "NVME",
		UserID: "test-user", Status: draftStatus, Role: "BOOT",
		VolumeGb: uint64(create.VolumeGb), OSImage: image,
	}
	s.storageMap[id] = st
	rec.BootDisk = id
	s.writeJSON(w, http.StatusOK, publicStorageWire(st))
}

func (s *Server) replaceDraftSSHKeys(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	var body struct {
		SSHKeys []string `json:"sshKeys"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.SSHKeys = body.SSHKeys
	s.writeJSON(w, http.StatusOK, vmWire(rec))
}

// attachDraftDataDisk serves POST /v3/vms/{id}/storages. The provider only
// sends the existing-disk variant (a bare storage-ID string); the create
// variant is accepted so the contract path stays exercised.
func (s *Server) attachDraftDataDisk(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	var raw json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	var existing string
	if json.Unmarshal(raw, &existing) == nil {
		st := s.adoptStorage(existing, rec.ClusterID, "DATA")
		rec.DataDisks = append(rec.DataDisks, existing)
		s.writeJSON(w, http.StatusOK, publicStorageWire(st))
		return
	}
	var create struct {
		VolumeGb   uint32  `json:"volumeGb"`
		Replicated bool    `json:"replicated"`
		Name       *string `json:"name"`
	}
	_ = json.Unmarshal(raw, &create)
	id := newID()
	name := rec.Name + "-data"
	if create.Name != nil {
		name = *create.Name
	}
	st := &storageRecord{
		ID: id, ClusterID: rec.ClusterID, Name: name, StorageType: "NVME",
		UserID: "test-user", Status: draftStatus, Role: "DATA",
		VolumeGb: uint64(create.VolumeGb), Replicated: create.Replicated,
	}
	s.storageMap[id] = st
	rec.DataDisks = append(rec.DataDisks, id)
	s.writeJSON(w, http.StatusOK, publicStorageWire(st))
}

// adoptStorage returns the storage record for id, synthesizing a ready one
// when tests reference a literal UUID that was never created through the
// mock. Caller holds s.mu.
func (s *Server) adoptStorage(id, clusterID, role string) *storageRecord {
	if st, ok := s.storageMap[id]; ok {
		return st
	}
	st := &storageRecord{
		ID: id, ClusterID: clusterID, Name: "adopted-" + role, StorageType: "NVME",
		UserID: "test-user", Status: "ready", Role: role, VolumeGb: draftBootDiskGb,
	}
	s.storageMap[id] = st
	return st
}

func (s *Server) provisionVMDraft(w http.ResponseWriter, rec *vmRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.Status = "launched"
	rec.RestartRequired = false
	if st, ok := s.storageMap[rec.BootDisk]; ok && st.Status == draftStatus {
		st.Status = "ready"
	}
	for _, ni := range rec.Interfaces {
		if ip, ok := s.publicIPMap[ni.PublicIP]; ok && ip.Status == draftStatus {
			ip.Status = "ready"
			ip.AttachedTo = rec.ID
		}
	}
	s.writeJSON(w, http.StatusOK, vmWire(rec))
}

// detachDataDisk serves DELETE /v3/vms/{id}/storages/{storage_id}: the disk
// leaves the VM, and a draft-created one is hard-deleted with it.
func (s *Server) detachDataDisk(w http.ResponseWriter, rec *vmRecord, storageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailRemoveVMStorages {
		s.writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"error": "injected: storages detach failure", "code": "service_unavailable"})
		return
	}
	kept := rec.DataDisks[:0]
	found := false
	for _, id := range rec.DataDisks {
		if id == storageID {
			found = true
			continue
		}
		kept = append(kept, id)
	}
	rec.DataDisks = kept
	if !found {
		s.writeError(w, "storage not attached")
		return
	}
	if st, ok := s.storageMap[storageID]; ok && st.Status == draftStatus {
		delete(s.storageMap, storageID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// The draft create body's sub-shapes.
type (
	draftDiskRef struct {
		StorageID string `json:"storageId"`
	}
	draftBootDiskBody struct {
		Kind      string  `json:"kind"`
		StorageID string  `json:"storageId"`
		VolumeGb  uint64  `json:"volumeGb"`
		Name      *string `json:"name"`
		Source    struct {
			Type    string `json:"type"`
			ImageID string `json:"imageId"`
			URL     string `json:"url"`
		} `json:"source"`
	}
	draftInterfaceBody struct {
		Kind            string   `json:"kind"`
		SubnetID        string   `json:"subnetId"`
		AddressType     string   `json:"addressType"`
		SecurityGroupID *string  `json:"securityGroupId"`
		StaticIPs       []string `json:"staticIps"`
		Default         bool     `json:"default"`
	}
)

func dataDiskRefs(disks []draftDiskRef) []string {
	out := make([]string, 0, len(disks))
	for _, d := range disks {
		out = append(out, d.StorageID)
	}
	return out
}

// seedDraftDisks gives the draft its boot disk — an adopted one or a new
// draft disk — plus its data disks, and names the storages it created.
func (s *Server) seedDraftDisks(rec *vmRecord, boot *draftBootDiskBody, dataDisks []string) []string {
	created := []string{}
	if boot != nil && boot.StorageID != "" {
		rec.BootDisk = boot.StorageID
		s.adoptStorage(boot.StorageID, rec.ClusterID, "BOOT")
	} else {
		id := newID()
		s.storageMap[id] = newDraftBootDisk(id, rec, boot)
		rec.BootDisk = id
		created = append(created, id)
	}
	for _, storageID := range dataDisks {
		s.adoptStorage(storageID, rec.ClusterID, "DATA")
		rec.DataDisks = append(rec.DataDisks, storageID)
	}
	return created
}

// seedDraftInterfaces builds the draft's interfaces: an omitted list means the
// automatic default one, an empty list means none.
func (s *Server) seedDraftInterfaces(rec *vmRecord, want *[]draftInterfaceBody) []string {
	createdIPs := []string{}
	if want == nil {
		rec.Interfaces = []vmInterfaceRecord{newDefaultInterface()}
		return createdIPs
	}
	for _, i := range *want {
		ni := vmInterfaceRecord{
			ID: newID(), Default: i.Default, Subnet: i.SubnetID,
			SecurityGroupID: i.SecurityGroupID, StaticIPs: i.StaticIPs,
		}
		if i.Kind == interfaceKindPublic {
			ni.Subnet = ""
			ni.PublicIP = s.newDraftPublicIP(rec)
			createdIPs = append(createdIPs, ni.PublicIP)
		}
		rec.Interfaces = append(rec.Interfaces, ni)
	}
	return createdIPs
}

// newDraftBootDisk builds the draft boot disk the create body asked for,
// falling back to the platform default when it named none.
func newDraftBootDisk(id string, rec *vmRecord, boot *draftBootDiskBody) *storageRecord {
	st := &storageRecord{
		ID: id, ClusterID: rec.ClusterID, Name: rec.Name + "-boot",
		StorageType: "NVME", UserID: "test-user", Status: draftStatus, Role: "BOOT",
		VolumeGb: draftBootDiskGb,
	}
	if boot == nil {
		return st
	}
	if boot.VolumeGb > 0 {
		st.VolumeGb = boot.VolumeGb
	}
	if boot.Name != nil {
		st.Name = *boot.Name
	}
	st.OSImage = boot.Source.ImageID
	if boot.Source.Type == interfaceKindHTTP {
		st.OSImage = boot.Source.URL
	}
	return st
}

// serveLiveVMRoute handles the /v3 routes that serve a live VM as well as a
// draft — reading it, and attaching or detaching its data disks. Reports
// whether it answered.
func (s *Server) serveLiveVMRoute(w http.ResponseWriter, r *http.Request, rec *vmRecord, parts []string) bool {
	switch {
	case len(parts) == resourcePathParts && r.Method == http.MethodGet:
		s.mu.Lock()
		wire := vmWire(rec)
		s.mu.Unlock()
		s.writeJSON(w, http.StatusOK, wire)
	case len(parts) == draftVerbPathParts && parts[3] == storagesVerb && r.Method == http.MethodPost:
		s.attachDraftDataDisk(w, r, rec)
	case len(parts) == draftVerbPathParts+1 && parts[3] == storagesVerb && r.Method == http.MethodDelete:
		s.detachDataDisk(w, rec, parts[4])
	default:
		return false
	}
	return true
}
