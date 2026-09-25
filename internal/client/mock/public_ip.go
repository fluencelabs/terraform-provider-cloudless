package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// lastOctetMod keeps a synthesized address's final octet within 0-254.
const lastOctetMod = 255

type publicIPRecord struct {
	ID, ClusterID, Name, AddressType, UserID, Status string
	Address                                          string // synthesized
	// AttachedTo is the id of the VM whose public interface holds this IP.
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

	s.mux.HandleFunc("/v3/public-ips", s.handlePublicIPCollection)
	s.mux.HandleFunc("/v3/public-ips/", s.handlePublicIPItem)
}

func (s *Server) handlePublicIPCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createPublicIP(w, r)
	case http.MethodGet:
		s.listPublicIPs(w, r)
	default:
		s.notFound(w, r)
	}
}

func (s *Server) createPublicIP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID   string `json:"clusterId"`
		Name        string `json:"name"`
		AddressType string `json:"addressType"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	s.publicIPSeq++
	rec := &publicIPRecord{
		ID: id, ClusterID: body.ClusterID, Name: body.Name,
		AddressType: body.AddressType, UserID: "test-user", Status: "ready",
		Address: fmt.Sprintf("203.0.113.%d", s.publicIPSeq%lastOctetMod),
	}
	s.publicIPMap[id] = rec
	s.writeJSON(w, http.StatusOK, publicIPWire(rec))
}

func (s *Server) listPublicIPs(w http.ResponseWriter, r *http.Request) {
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

// handlePublicIPItem serves PATCH /v1/public_ips/{id}.
func (s *Server) handlePublicIPItem(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) != resourcePathParts {
		s.notFound(w, r)
		return
	}
	id := parts[2]
	if r.Method == http.MethodDelete {
		s.deletePublicIP(w, id)
		return
	}
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
	rec, ok := s.publicIPMap[id]
	if !ok {
		s.writeError(w, "public ip not found")
		return
	}
	if body.Name != nil {
		rec.Name = *body.Name
	}
	s.writeJSON(w, http.StatusOK, publicIPWire(rec))
}

func publicIPWire(rec *publicIPRecord) map[string]any {
	out := map[string]any{
		"id":          rec.ID,
		"clusterId":   rec.ClusterID,
		"name":        rec.Name,
		"addressType": rec.AddressType,
		"status":      rec.Status,
		"createdAt":   "2026-01-01T00:00:00Z",
		"updatedAt":   "2026-01-01T00:00:00Z",
	}
	if rec.Address != "" {
		out["address"] = rec.Address
	}
	if rec.AttachedTo != "" {
		// PublicIpDto names the holding VM by id alone.
		out["vmId"] = rec.AttachedTo
	}
	return out
}

// deletePublicIP serves DELETE /{id}: /v3 deletes one resource at a time and
// answers with the object it removed.
func (s *Server) deletePublicIP(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.publicIPMap[id]
	if !ok {
		s.writeError(w, "public ip not found")
		return
	}
	wire := publicIPWire(rec)
	delete(s.publicIPMap, id)
	s.writeJSON(w, http.StatusOK, wire)
}
