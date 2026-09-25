package mock

import (
	"encoding/json"
	"net/http"
)

type storageRecord struct {
	ID, ClusterID, Name, StorageType, UserID, Status, Role string
	VolumeGb                                               uint64
	OSImage                                                string
	Replicated                                             bool
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

	s.mux.HandleFunc("/v3/storages", s.handleStorageCollection)
	s.mux.HandleFunc("/v3/storages/", s.handleStorageItem)
}

func (s *Server) handleStorageCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createStorage(w, r)
	case http.MethodGet:
		s.listStorages(w, r)
	default:
		s.notFound(w, r)
	}
}

func (s *Server) createStorage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID   string `json:"clusterId"`
		Name        string `json:"name"`
		StorageType string `json:"storageType"`
		Source      *struct {
			Type    string `json:"type"`
			ImageID string `json:"imageId"`
			URL     string `json:"url"`
		} `json:"source"`
		VolumeGb   uint32 `json:"volumeGb"`
		Replicated bool   `json:"replicated"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	role := "DATA"
	image := ""
	if body.Source != nil {
		image = body.Source.ImageID
		if body.Source.Type == "http" {
			image = body.Source.URL
		}
	}
	if image != "" {
		role = "BOOT"
	}
	rec := &storageRecord{
		ID: id, ClusterID: body.ClusterID, Name: body.Name,
		StorageType: body.StorageType, UserID: "test-user", Status: "ready",
		Role: role, VolumeGb: uint64(body.VolumeGb), OSImage: image,
		Replicated: body.Replicated,
	}
	s.storageMap[id] = rec
	s.writeJSON(w, http.StatusOK, publicStorageWire(rec))
}

func (s *Server) listStorages(w http.ResponseWriter, r *http.Request) {
	want := r.URL.Query().Get("ids")
	items := []map[string]any{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, st := range s.storageMap {
		if want != "" && id != want {
			continue
		}
		items = append(items, publicStorageWire(st))
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

// handleStorageItem serves PATCH /v1/storages/{id}.
func (s *Server) handleStorageItem(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) != resourcePathParts {
		s.notFound(w, r)
		return
	}
	id := parts[2]
	if r.Method == http.MethodDelete {
		s.deleteStorage(w, id)
		return
	}
	if r.Method != http.MethodPatch {
		s.notFound(w, r)
		return
	}
	var body struct {
		Name     *string `json:"name"`
		VolumeGb *uint32 `json:"volumeGb"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.storageMap[id]
	if !ok {
		s.writeError(w, "storage not found")
		return
	}
	if body.Name != nil {
		rec.Name = *body.Name
	}
	if body.VolumeGb != nil {
		rec.VolumeGb = uint64(*body.VolumeGb)
	}
	s.writeJSON(w, http.StatusOK, publicStorageWire(rec))
}

// publicStorageWire is PublicStorageDto — the shape /v3 answers with. It
// stands beside storageWire while /v1 still serves the storage resource.
func publicStorageWire(rec *storageRecord) map[string]any {
	out := map[string]any{
		"id":            rec.ID,
		"clusterId":     rec.ClusterID,
		"name":          rec.Name,
		"storageType":   rec.StorageType,
		"status":        rec.Status,
		"volumeGb":      rec.VolumeGb,
		"replicated":    rec.Replicated,
		"attachedVmIds": []string{},
		"createdAt":     "2026-01-01T00:00:00Z",
		"updatedAt":     "2026-01-01T00:00:00Z",
	}
	if rec.OSImage != "" {
		out["imageId"] = rec.OSImage
		out["bootMode"] = "EFI"
	}
	return out
}

// deleteStorage serves DELETE /{id}: /v3 deletes one resource at a time and
// answers with the object it removed.
func (s *Server) deleteStorage(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.storageMap[id]
	if !ok {
		s.writeError(w, "storage not found")
		return
	}
	wire := publicStorageWire(rec)
	delete(s.storageMap, id)
	s.writeJSON(w, http.StatusOK, wire)
}
