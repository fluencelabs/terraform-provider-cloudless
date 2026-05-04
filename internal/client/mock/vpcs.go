package mock

import (
	"encoding/json"
	"net/http"
)

// vpcRecord is the mock's in-memory shape for a VPC.
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

	s.mux.HandleFunc("/v1/vpcs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ClusterID      string `json:"clusterId"`
				Name           string `json:"name"`
				EnableExternal *bool  `json:"enableExternal,omitempty"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
			id := newID()
			rec := &vpcRecord{
				ID:             id,
				Name:           body.Name,
				ClusterID:      body.ClusterID,
				UserID:         "test-user",
				Status:         "ready",
				EnableExternal: body.EnableExternal,
			}
			s.vpcMap[id] = rec
			s.writeJSON(w, http.StatusOK, vpcWire(rec))
		case http.MethodGet:
			s.handleVPCsGet(w, r)
		default:
			s.notFound(w, r)
		}
	})
	// /v1/vpcs/delete is an exact path; ServeMux prefers exact matches over
	// the /v1/vpcs/ prefix, so register both.
	s.mux.HandleFunc("/v1/vpcs/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.notFound(w, r)
			return
		}
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		for _, id := range body.IDs {
			delete(s.vpcMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	s.mux.HandleFunc("/v1/vpcs/", func(w http.ResponseWriter, r *http.Request) {
		// PATCH /v1/vpcs/{id}
		if r.Method != http.MethodPatch {
			s.notFound(w, r)
			return
		}
		parts := splitPath(r.URL.Path)
		if len(parts) != 3 {
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
			s.writeError(w, http.StatusNotFound, "vpc not found")
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
		"items":      items,
		"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
	})
}

func vpcWire(rec *vpcRecord) map[string]any {
	out := map[string]any{
		"id":           rec.ID,
		"name":         rec.Name,
		"clusterId":    rec.ClusterID,
		"userId":       rec.UserID,
		"status":       rec.Status,
		"subnetsCount": 0,
		"createdAt":    "2026-01-01T00:00:00Z",
	}
	if rec.EnableExternal != nil {
		out["enableExternal"] = *rec.EnableExternal
	}
	return out
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
