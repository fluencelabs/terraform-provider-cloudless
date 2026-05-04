package mock

import (
	"encoding/json"
	"net/http"
)

type vmRecord struct {
	ID, ClusterID, ConfigurationID, Name, UserID, Status, BootDisk string
	DataDisks                                                      []string
	SSHKeys                                                        []string
	Subnets                                                        []string
	Interfaces                                                     []vmInterfaceRecord
	PublicIP                                                       string
	CreatedAt, UpdatedAt                                           string
}

type vmInterfaceRecord struct {
	ID              string
	SecurityGroupID *string
}

// wireVMsOnce is called from New() to register VM handlers idempotently. Per
// the defensive-lock pattern: do NOT hold s.mu around the sync.Once.Do call;
// wireVMs locks briefly only for map init, then performs mutex-free mux
// registration.
func (s *Server) wireVMsOnce() { s.vmWiring.Do(s.wireVMs) }

func (s *Server) wireVMs() {
	s.mu.Lock()
	if s.vmMap == nil {
		s.vmMap = map[string]*vmRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v2/vms", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
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
				// Inline create: synthesize a storage ID.
				rec.BootDisk = newID()
			}
			// Auto-create one network interface so the network_interface_ids
			// list is populated from a Get response.
			rec.Interfaces = []vmInterfaceRecord{{ID: newID()}}
			s.vmMap[id] = rec
			out := vmWire(rec)
			s.writeJSON(w, http.StatusOK, out)
		case http.MethodGet:
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
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	s.mux.HandleFunc("/v2/vms/", func(w http.ResponseWriter, r *http.Request) {
		// Subpaths: /v2/vms/{id}, /v2/vms/{id}/storages/{add|remove},
		// /v2/vms/{id}/public_ip/{add|remove}, /v2/vms/{id}/interfaces,
		// /v2/vms/{id}/interfaces/{interface_id}, /v2/vms/{id}/terminate.
		parts := splitPath(r.URL.Path)
		if len(parts) < 3 {
			s.notFound(w, r)
			return
		}
		vmID := parts[2]
		s.mu.Lock()
		rec, ok := s.vmMap[vmID]
		s.mu.Unlock()
		if !ok {
			s.writeError(w, http.StatusNotFound, "vm not found")
			return
		}
		switch {
		case len(parts) == 3 && r.Method == http.MethodPatch:
			var body struct {
				Name *string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
			if body.Name != nil {
				rec.Name = *body.Name
			}
			out := vmWire(rec)
			s.writeJSON(w, http.StatusOK, out)
		case len(parts) == 4 && parts[3] == "terminate" && r.Method == http.MethodPost:
			s.mu.Lock()
			delete(s.vmMap, vmID)
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 5 && parts[3] == "storages" && parts[4] == "add" && r.Method == http.MethodPost:
			var body struct {
				DataDisks []string `json:"dataDisks"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			rec.DataDisks = append(rec.DataDisks, body.DataDisks...)
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 5 && parts[3] == "storages" && parts[4] == "remove" && r.Method == http.MethodPost:
			s.mu.Lock()
			fail := s.FailRemoveVMStorages
			s.mu.Unlock()
			if fail {
				s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "injected: storages/remove failure"})
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
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 5 && parts[3] == "public_ip" && parts[4] == "add" && r.Method == http.MethodPost:
			var body struct {
				PublicIPID string `json:"publicIpId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			rec.PublicIP = body.PublicIPID
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 5 && parts[3] == "public_ip" && parts[4] == "remove" && r.Method == http.MethodPost:
			s.mu.Lock()
			rec.PublicIP = ""
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 4 && parts[3] == "interfaces" && r.Method == http.MethodGet:
			s.mu.Lock()
			defer s.mu.Unlock()
			out := make([]map[string]any, 0, len(rec.Interfaces))
			for _, ni := range rec.Interfaces {
				m := map[string]any{"id": ni.ID}
				if ni.SecurityGroupID != nil {
					m["securityGroupId"] = *ni.SecurityGroupID
				}
				out = append(out, m)
			}
			s.writeJSON(w, http.StatusOK, out)
		case len(parts) == 5 && parts[3] == "interfaces" && r.Method == http.MethodPatch:
			interfaceID := parts[4]
			var body struct {
				SecurityGroupID *string `json:"securityGroupId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
			for i := range rec.Interfaces {
				if rec.Interfaces[i].ID == interfaceID {
					rec.Interfaces[i].SecurityGroupID = body.SecurityGroupID
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			s.writeError(w, http.StatusNotFound, "interface not found")
		default:
			s.notFound(w, r)
		}
	})
}

func vmWire(rec *vmRecord) map[string]any {
	out := map[string]any{
		"id":              rec.ID,
		"userId":          rec.UserID,
		"clusterId":       rec.ClusterID,
		"configurationId": rec.ConfigurationID,
		"name":            rec.Name,
		"status":          rec.Status,
		"restartRequired": false,
		"dataDisks":       rec.DataDisks,
		"subnets":         rec.Subnets,
		"sshKeys":         rec.SSHKeys,
		"createdAt":       rec.CreatedAt,
		"updatedAt":       rec.UpdatedAt,
	}
	ifaceIDs := make([]string, 0, len(rec.Interfaces))
	for _, ni := range rec.Interfaces {
		ifaceIDs = append(ifaceIDs, ni.ID)
	}
	out["networkInterfaces"] = ifaceIDs
	if rec.BootDisk != "" {
		out["bootDisk"] = rec.BootDisk
	}
	if rec.PublicIP != "" {
		out["publicIp"] = rec.PublicIP
	}
	return out
}
