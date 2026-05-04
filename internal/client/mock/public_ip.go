package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
)

// publicIPCounter assigns sequential octets to synthesized addresses so the
// mock returns valid IPv4 strings.
var publicIPCounter atomic.Uint32

type publicIPRecord struct {
	ID, ClusterID, Name, AddressType, UserID, Status string
	Address                                          string // synthesized
	// AttachedTo is set by the vm-public-ip-attachment handler (Task 8).
	AttachedTo string
}

// wirePublicIPsOnce is called from New() to register public IP handlers
// idempotently. Per the defensive-lock pattern: do NOT hold s.mu around
// the sync.Once.Do call; wirePublicIPs locks briefly only for map init,
// then performs mutex-free mux registration.
func (s *Server) wirePublicIPsOnce() { s.publicIPWiring.Do(s.wirePublicIPs) }

func (s *Server) wirePublicIPs() {
	s.mu.Lock()
	if s.publicIPMap == nil {
		s.publicIPMap = map[string]*publicIPRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v1/public_ips", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ClusterID   string `json:"clusterId"`
				Name        string `json:"name"`
				AddressType string `json:"addressType"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
			id := newID()
			rec := &publicIPRecord{
				ID: id, ClusterID: body.ClusterID, Name: body.Name,
				AddressType: body.AddressType, UserID: "test-user", Status: "ready",
				Address: fmt.Sprintf("203.0.113.%d", publicIPCounter.Add(1)%255),
			}
			s.publicIPMap[id] = rec
			out := publicIPWire(rec)
			s.writeJSON(w, http.StatusOK, out)
		case http.MethodGet:
			want := r.URL.Query().Get("ids")
			items := []map[string]any{}
			s.mu.Lock()
			defer s.mu.Unlock()
			for id, p := range s.publicIPMap {
				if want != "" && id != want {
					continue
				}
				items = append(items, publicIPWire(p))
			}
			s.writeJSON(w, http.StatusOK, map[string]any{
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	// /v1/public_ips/delete is an exact path; ServeMux prefers exact matches
	// over the /v1/public_ips/ prefix, so register both.
	s.mux.HandleFunc("/v1/public_ips/delete", func(w http.ResponseWriter, r *http.Request) {
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
			delete(s.publicIPMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	s.mux.HandleFunc("/v1/public_ips/", func(w http.ResponseWriter, r *http.Request) {
		// PATCH /v1/public_ips/{id}
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
			Name *string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.publicIPMap[id]
		if !ok {
			s.writeError(w, http.StatusNotFound, "public ip not found")
			return
		}
		if body.Name != nil {
			rec.Name = *body.Name
		}
		out := publicIPWire(rec)
		s.writeJSON(w, http.StatusOK, out)
	})
}

func publicIPWire(rec *publicIPRecord) map[string]any {
	out := map[string]any{
		"id":          rec.ID,
		"clusterId":   rec.ClusterID,
		"name":        rec.Name,
		"addressType": rec.AddressType,
		"userId":      rec.UserID,
		"status":      rec.Status,
		"createdAt":   "2026-01-01T00:00:00Z",
	}
	if rec.Address != "" {
		out["address"] = rec.Address
	}
	if rec.AttachedTo != "" {
		out["attachedTo"] = rec.AttachedTo
	}
	return out
}
