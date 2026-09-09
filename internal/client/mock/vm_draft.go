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
		s.createVMDraft(w)
	})
	s.mux.HandleFunc("/v3/vms/", s.handleVMDraftItem)
}

func (s *Server) createVMDraft(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageMap == nil {
		s.storageMap = map[string]*storageRecord{}
	}
	id := newID()
	bootID := newID()
	s.storageMap[bootID] = &storageRecord{
		ID: bootID, ClusterID: defaultDraftClusterID, Name: "draft-boot",
		StorageType: "NVME", UserID: "test-user", Status: draftStatus, Role: "BOOT",
		VolumeGb: draftBootDiskGb,
	}
	rec := &vmRecord{
		ID: id, ClusterID: defaultDraftClusterID, ConfigurationID: newID(),
		Name: draftStatus, UserID: "test-user", Status: draftStatus, BootDisk: bootID,
		DataDisks: []string{}, SSHKeys: []string{},
		Interfaces: []vmInterfaceRecord{newDefaultInterface()},
		CreatedAt:  "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	s.vmMap[id] = rec
	s.writeJSON(w, http.StatusOK, vmWire(rec))
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
	// Interface routes serve drafts and live VMs alike.
	if len(parts) >= draftVerbPathParts && parts[3] == interfacesVerb {
		s.handleVMInterfaceV3(w, r, rec, parts[4:])
		return
	}
	if rec.Status != draftStatus {
		s.writeJSON(w, http.StatusUnprocessableEntity,
			map[string]string{"error": "vm is not a draft", "code": "vm_not_draft"})
		return
	}
	switch {
	case len(parts) == resourcePathParts && r.Method == http.MethodPatch:
		s.updateVMDraft(w, r, rec)
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
// disk) or {volumeGb, imageId, name?} (new draft disk from a catalog image).
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
		s.writeJSON(w, http.StatusOK, storageWire(st))
		return
	}
	var create struct {
		VolumeGb uint32  `json:"volumeGb"`
		ImageID  string  `json:"imageId"`
		Name     *string `json:"name"`
	}
	_ = json.Unmarshal(raw, &create)
	name := rec.Name + "-boot"
	if create.Name != nil {
		name = *create.Name
	}
	id := newID()
	st := &storageRecord{
		ID: id, ClusterID: rec.ClusterID, Name: name, StorageType: "NVME",
		UserID: "test-user", Status: draftStatus, Role: "BOOT",
		VolumeGb: uint64(create.VolumeGb), OSImage: create.ImageID,
	}
	s.storageMap[id] = st
	rec.BootDisk = id
	s.writeJSON(w, http.StatusOK, storageWire(st))
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
		s.writeJSON(w, http.StatusOK, storageWire(st))
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
	s.writeJSON(w, http.StatusOK, storageWire(st))
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
