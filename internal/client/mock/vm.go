package mock

import (
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
const ()

func (s *Server) wireVMs() {
	s.mu.Lock()
	if s.vmMap == nil {
		s.vmMap = map[string]*vmRecord{}
	}
	s.mu.Unlock()

	s.wireVMDrafts()
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

// terminateVM answers 202 with the TerminatedVm receipt: the id, the status
// the request was accepted in, and the resources that outlive the VM.
func (s *Server) terminateVM(w http.ResponseWriter, rec *vmRecord) {
	s.mu.Lock()
	retained := map[string]any{"storageIds": []string{}, "publicIpIds": []string{}}
	delete(s.vmMap, rec.ID)
	s.mu.Unlock()
	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"id": rec.ID, "acceptedStatus": "terminating", "retainedResources": retained,
	})
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

func vmWire(rec *vmRecord) map[string]any {
	ifaceIDs := make([]string, 0, len(rec.Interfaces))
	for _, ni := range rec.Interfaces {
		ifaceIDs = append(ifaceIDs, ni.ID)
	}
	out := map[string]any{
		"id":              rec.ID,
		"clusterId":       rec.ClusterID,
		"configurationId": rec.ConfigurationID,
		"name":            rec.Name,
		"status":          rec.Status,
		"restartRequired": rec.RestartRequired,
		"hasCloudInit":    false,
		"dataDiskIds":     rec.DataDisks,
		"sshKeyIds":       rec.SSHKeys,
		"interfaceIds":    ifaceIDs,
		"createdAt":       rec.CreatedAt,
		"updatedAt":       rec.UpdatedAt,
	}
	if rec.BootDisk != "" {
		out["bootDiskId"] = rec.BootDisk
	}
	return out
}
