package mock

import (
	"encoding/json"
	"net/http"
)

type storageRecord struct {
	ID, ClusterID, Name, StorageType, UserID, Status, Role string
	VolumeGb                                               uint64
	OSImage                                                string
}

// wireStoragesOnce is called from New() to register storage handlers
// idempotently. Per the defensive-lock pattern: do NOT hold s.mu around
// the sync.Once.Do call; wireStorages locks briefly only for map init,
// then performs mutex-free mux registration.
func (s *Server) wireStoragesOnce() { s.storageWiring.Do(s.wireStorages) }

func (s *Server) wireStorages() {
	s.mu.Lock()
	if s.storageMap == nil {
		s.storageMap = map[string]*storageRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v1/storages", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ClusterID   string `json:"clusterId"`
				Name        string `json:"name"`
				StorageType string `json:"storageType"`
				OSImage     string `json:"osImage"`
				VolumeGb    uint32 `json:"volumeGb"`
				Replicated  bool   `json:"replicated"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
			id := newID()
			role := "DATA"
			if body.OSImage != "" {
				role = "BOOT"
			}
			rec := &storageRecord{
				ID: id, ClusterID: body.ClusterID, Name: body.Name,
				StorageType: body.StorageType, UserID: "test-user", Status: "ready",
				Role: role, VolumeGb: uint64(body.VolumeGb), OSImage: body.OSImage,
			}
			s.storageMap[id] = rec
			s.writeJSON(w, http.StatusOK, storageWire(rec))
		case http.MethodGet:
			want := r.URL.Query().Get("ids")
			items := []map[string]any{}
			s.mu.Lock()
			defer s.mu.Unlock()
			for id, st := range s.storageMap {
				if want != "" && id != want {
					continue
				}
				items = append(items, storageWire(st))
			}
			s.writeJSON(w, http.StatusOK, map[string]any{
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	// /v1/storages/delete is an exact path; ServeMux prefers exact matches over
	// the /v1/storages/ prefix, so register both.
	s.mux.HandleFunc("/v1/storages/delete", func(w http.ResponseWriter, r *http.Request) {
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
			delete(s.storageMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	s.mux.HandleFunc("/v1/storages/", func(w http.ResponseWriter, r *http.Request) {
		// PATCH /v1/storages/{id}
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
			Name     *string `json:"name"`
			VolumeGb *uint32 `json:"volumeGb"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.storageMap[id]
		if !ok {
			s.writeError(w, http.StatusNotFound, "storage not found")
			return
		}
		if body.Name != nil {
			rec.Name = *body.Name
		}
		if body.VolumeGb != nil {
			rec.VolumeGb = uint64(*body.VolumeGb)
		}
		out := storageWire(rec)
		s.writeJSON(w, http.StatusOK, out)
	})
}

func storageWire(rec *storageRecord) map[string]any {
	return map[string]any{
		"id":          rec.ID,
		"clusterId":   rec.ClusterID,
		"name":        rec.Name,
		"storageType": rec.StorageType,
		"userId":      rec.UserID,
		"status":      rec.Status,
		"role":        rec.Role,
		"volumeGb":    rec.VolumeGb,
		"attachedTo":  []string{},
		"createdAt":   "2026-01-01T00:00:00Z",
	}
}
