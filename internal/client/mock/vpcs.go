package mock

import (
	"encoding/json"
	"net/http"
)

// vpcRecord is the mock's in-memory shape for a VPC.
// clusterOfVPC returns the cluster a VPC lives on, as the API derives it for
// subnets and security groups. Unknown VPCs (tests using literal UUIDs) fall
// back to the draft default cluster so responses stay contract-valid.
// Caller holds s.mu.
func (s *Server) clusterOfVPC(vpcID string) string {
	if v, ok := s.vpcMap[vpcID]; ok && v.ClusterID != "" {
		return v.ClusterID
	}
	return defaultDraftClusterID
}

type vpcRecord struct {
	ID, Name, ClusterID, UserID, Status string
	EnableExternal                      *bool
}

func (s *Server) wireVPCsOnce() { s.vpcWiringOnce.Do(s.wireVPCs) }

func (s *Server) wireVPCs() {
	s.mu.Lock()
	if s.vpcMap == nil {
		s.vpcMap = map[string]*vpcRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v3/vpcs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ClusterID string `json:"clusterId"`
				Name      string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
			id := newID()
			rec := &vpcRecord{
				ID:        id,
				Name:      body.Name,
				ClusterID: body.ClusterID,
				UserID:    "test-user",
				Status:    "ready",
			}
			s.vpcMap[id] = rec
			s.writeJSON(w, http.StatusOK, vpcWire(rec))
		case http.MethodGet:
			s.handleVPCsGet(w, r)
		default:
			s.notFound(w, r)
		}
	})
	s.mux.HandleFunc("/v3/vpcs/", func(w http.ResponseWriter, r *http.Request) {
		// POST /v3/vpcs/{id}/subnets shares the prefix; hand it to subnets.
		if p := splitPath(r.URL.Path); len(p) == subnetCreatePathParts && p[3] == "subnets" {
			s.handleSubnetCreate(w, r)
			return
		}
		if p := splitPath(r.URL.Path); len(p) == resourcePathParts && r.Method == http.MethodDelete {
			s.deleteVPC(w, p[2])
			return
		}
		// PATCH /v3/vpcs/{id}
		if r.Method != http.MethodPatch {
			s.notFound(w, r)
			return
		}
		parts := splitPath(r.URL.Path)
		if len(parts) != resourcePathParts {
			s.notFound(w, r)
			return
		}
		id := parts[2]
		var body struct {
			Name           *string `json:"name,omitempty"`
			EnableExternal *bool   `json:"enableExternal,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.vpcMap[id]
		if !ok {
			s.writeError(w, "vpc not found")
			return
		}
		if body.Name != nil {
			rec.Name = *body.Name
		}
		if body.EnableExternal != nil {
			rec.EnableExternal = body.EnableExternal
		}
		s.writeJSON(w, http.StatusOK, vpcWire(rec))
	})
}

func (s *Server) handleVPCsGet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := r.URL.Query().Get("ids")
	items := []map[string]any{}
	for id, v := range s.vpcMap {
		if want != "" && id != want {
			continue
		}
		items = append(items, vpcWire(v))
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

func vpcWire(rec *vpcRecord) map[string]any {
	return map[string]any{
		"id":        rec.ID,
		"name":      rec.Name,
		"clusterId": rec.ClusterID,
		"status":    rec.Status,
		"createdAt": "2026-01-01T00:00:00Z",
	}
}

// SeedVPC inserts a VPC record. Tests use this to set up parent-VPC state
// for the subnet resolver.
func (s *Server) SeedVPC(id, name, clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vpcMap == nil {
		s.vpcMap = map[string]*vpcRecord{}
	}
	s.vpcMap[id] = &vpcRecord{ID: id, Name: name, ClusterID: clusterID, UserID: "test-user", Status: "ready"}
}

// deleteVPC serves DELETE /v3/vpcs/{id}.
func (s *Server) deleteVPC(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.vpcMap[id]
	if !ok {
		s.writeError(w, "vpc not found")
		return
	}
	wire := vpcWire(rec)
	delete(s.vpcMap, id)
	s.writeJSON(w, http.StatusOK, wire)
}
