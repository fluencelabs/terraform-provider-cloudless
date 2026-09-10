package mock

import (
	"encoding/json"
	"net/http"
)

type vmRecord struct {
	ID, ClusterID, ConfigurationID, Name, UserID, Status, BootDisk string
	DataDisks                                                      []string
	SSHKeys                                                        []string
	Interfaces                                                     []vmInterfaceRecord
	RestartRequired                                                bool
	CreatedAt, UpdatedAt                                           string
}

// wireVMsOnce is called from New() to register VM handlers idempotently. Per
// the defensive-lock pattern: do NOT hold s.mu around the sync.Once.Do call;
// wireVMs locks briefly only for map init, then performs mutex-free mux
// registration.
func (s *Server) wireVMsOnce() { s.vmWiring.Do(s.wireVMs) }

// VM subpath segment counts once split.
const (
	vmVerbPathParts        = 4 // /v2/vms/{id}/{verb}
	vmSubresourcePathParts = 5 // /v2/vms/{id}/{group}/{action}
)

func (s *Server) wireVMs() {
	s.mu.Lock()
	if s.vmMap == nil {
		s.vmMap = map[string]*vmRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v2/vms", s.handleVMCollection)
	s.mux.HandleFunc("/v2/vms/", s.handleVMItem)
	s.wireVMDrafts()
}

func (s *Server) handleVMCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createVM(w, r)
	case http.MethodGet:
		s.listVMs(w, r)
	default:
		s.notFound(w, r)
	}
}

func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID       string          `json:"clusterId"`
		ConfigurationID string          `json:"configurationId"`
		Name            string          `json:"name"`
		BootDisk        json.RawMessage `json:"bootDisk"`
		DataDisks       []string        `json:"dataDisks"`
		SSHKeys         []string        `json:"sshKeys"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	rec := &vmRecord{
		ID: id, ClusterID: body.ClusterID, ConfigurationID: body.ConfigurationID,
		Name: body.Name, UserID: "test-user", Status: "launched",
		DataDisks: body.DataDisks, SSHKeys: body.SSHKeys,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	// Decide bootDisk: if it parses as a JSON string, that's a storage ID.
	var asString string
	if json.Unmarshal(body.BootDisk, &asString) == nil {
		rec.BootDisk = asString
	} else {
		// Inline create: synthesize a boot storage volume and record it as a
		// first-class BOOT storage, mirroring the real API. VM terminate does
		// NOT cascade-delete it, so the provider must delete it explicitly on
		// destroy.
		bootID := newID()
		rec.BootDisk = bootID
		if s.storageMap == nil {
			s.storageMap = map[string]*storageRecord{}
		}
		s.storageMap[bootID] = &storageRecord{
			ID: bootID, ClusterID: body.ClusterID, Name: body.Name + "-boot",
			StorageType: "NVME", UserID: "test-user", Status: "ready", Role: "BOOT",
		}
	}
	// Auto-create one network interface so the network_interface_ids list is
	// populated from a Get response.
	rec.Interfaces = []vmInterfaceRecord{newDefaultInterface()}
	s.vmMap[id] = rec
	s.writeJSON(w, http.StatusOK, vmWire(rec))
}

func (s *Server) listVMs(w http.ResponseWriter, r *http.Request) {
	want := r.URL.Query().Get("ids")
	items := []map[string]any{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, vm := range s.vmMap {
		if want != "" && id != want {
			continue
		}
		items = append(items, vmWire(vm))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"pagination": map[string]int{
			"totalRecords":    len(items),
			"filteredRecords": len(items),
			"totalPages":      1,
			"currentPage":     0,
			"perPage":         defaultPerPage,
		},
	})
}

// handleVMItem dispatches every /v2/vms/{id}[/...] subpath. Shapes:
// /v2/vms/{id}, /v2/vms/{id}/terminate, /v2/vms/{id}/interfaces,
// /v2/vms/{id}/interfaces/{interface_id}, /v2/vms/{id}/storages/{add|remove},
// /v2/vms/{id}/public_ip/{add|remove}.
func (s *Server) handleVMItem(w http.ResponseWriter, r *http.Request) {
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
	switch len(parts) {
	case resourcePathParts:
		s.updateVM(w, r, rec)
	case vmVerbPathParts:
		s.handleVMVerb(w, r, rec, parts[3])
	case vmSubresourcePathParts:
		s.handleVMSubresource(w, r, rec, parts[3], parts[4])
	default:
		s.notFound(w, r)
	}
}

// updateVM serves PATCH /v2/vms/{id}.
func (s *Server) updateVM(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	if r.Method != http.MethodPatch {
		s.notFound(w, r)
		return
	}
	var body struct {
		Name *string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if body.Name != nil {
		rec.Name = *body.Name
	}
	s.writeJSON(w, http.StatusOK, vmWire(rec))
}

// handleVMVerb serves /v2/vms/{id}/{verb}.
func (s *Server) handleVMVerb(w http.ResponseWriter, r *http.Request, rec *vmRecord, verb string) {
	switch {
	case verb == "terminate" && r.Method == http.MethodPost:
		s.terminateVM(w, rec)
	case (verb == "restart" || verb == "softreboot") && r.Method == http.MethodPost:
		s.restartVM(w, rec)
	case verb == interfacesVerb && r.Method == http.MethodGet:
		s.listVMInterfaces(w, rec)
	default:
		s.notFound(w, r)
	}
}

// handleVMSubresource serves /v2/vms/{id}/{group}/{action}.
func (s *Server) handleVMSubresource(w http.ResponseWriter, r *http.Request, rec *vmRecord, group, action string) {
	switch {
	case group == storagesVerb && action == "add" && r.Method == http.MethodPost:
		s.addVMStorages(w, r, rec)
	case group == storagesVerb && action == "remove" && r.Method == http.MethodPost:
		s.removeVMStorages(w, r, rec)
	case group == interfacesVerb && r.Method == http.MethodPatch:
		s.patchVMInterface(w, r, rec, action)
	default:
		s.notFound(w, r)
	}
}

// terminateVM answers with the terminated VM, as the endpoint declares; the
// record is rendered before it is dropped.
func (s *Server) terminateVM(w http.ResponseWriter, rec *vmRecord) {
	s.mu.Lock()
	wire := vmWire(rec)
	delete(s.vmMap, rec.ID)
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, wire)
}

// restartVM serves POST /v2/vms/{id}/{restart,softreboot}. The real API clears
// restart_required and brings the VM back up; mirror that and count the call so
// tests can assert a restart actually happened.
func (s *Server) restartVM(w http.ResponseWriter, rec *vmRecord) {
	s.mu.Lock()
	rec.RestartRequired = false
	rec.Status = "launched"
	s.restartCount++
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, vmWire(rec))
}

func (s *Server) addVMStorages(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	var body struct {
		DataDisks []string `json:"dataDisks"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	rec.DataDisks = append(rec.DataDisks, body.DataDisks...)
	wire := vmWire(rec)
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, wire)
}

func (s *Server) removeVMStorages(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	s.mu.Lock()
	fail := s.FailRemoveVMStorages
	s.mu.Unlock()
	if fail {
		s.writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{"error": "injected: storages/remove failure", "code": "internal_server_error"},
		)
		return
	}
	var body struct {
		DataDisks []string `json:"dataDisks"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	drop := map[string]bool{}
	for _, id := range body.DataDisks {
		drop[id] = true
	}
	kept := rec.DataDisks[:0]
	for _, id := range rec.DataDisks {
		if !drop[id] {
			kept = append(kept, id)
		}
	}
	rec.DataDisks = kept
	wire := vmWire(rec)
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, wire)
}

func vmWire(rec *vmRecord) map[string]any {
	out := map[string]any{
		"id":              rec.ID,
		"userId":          rec.UserID,
		"clusterId":       rec.ClusterID,
		"configurationId": rec.ConfigurationID,
		"name":            rec.Name,
		"status":          rec.Status,
		"restartRequired": rec.RestartRequired,
		"allowedActions":  []string{},
		"dataDisks":       rec.DataDisks,
		"sshKeys":         rec.SSHKeys,
		"createdAt":       rec.CreatedAt,
		"updatedAt":       rec.UpdatedAt,
	}
	ifaceIDs := make([]string, 0, len(rec.Interfaces))
	subnets := []string{}
	for _, ni := range rec.Interfaces {
		ifaceIDs = append(ifaceIDs, ni.ID)
		switch {
		case ni.PublicIP != "":
			out["publicIp"] = ni.PublicIP
		case ni.Subnet != "":
			subnets = append(subnets, ni.Subnet)
		}
	}
	out["networkInterfaces"] = ifaceIDs
	out["subnets"] = subnets
	if rec.BootDisk != "" {
		out["bootDisk"] = rec.BootDisk
	}
	return out
}
