package mock

import (
	"encoding/json"
	"net/http"
)

type sgRecord struct {
	ID, ClusterID, Name, UserID, Status, VPCID string
	Ingress, Egress                            json.RawMessage
}

// wireSGsOnce is called from New() to register security group handlers
// idempotently. Per the defensive-lock pattern: do NOT hold s.mu around
// the sync.Once.Do call; wireSGs locks briefly only for map init, then
// performs mutex-free mux registration.
func (s *Server) wireSGsOnce() { s.sgWiring.Do(s.wireSGs) }

func (s *Server) wireSGs() {
	s.mu.Lock()
	if s.sgMap == nil {
		s.sgMap = map[string]*sgRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v1/security_groups", s.handleSGCollection)
	// /v1/security_groups/delete is an exact path; ServeMux prefers exact
	// matches over the /v1/security_groups/ prefix, so register both.
	s.mux.HandleFunc("/v1/security_groups/delete", s.handleSGBulkDelete)
	s.mux.HandleFunc("/v1/security_groups/", s.handleSGItem)
}

func (s *Server) handleSGCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createSG(w, r)
	case http.MethodGet:
		s.listSGs(w, r)
	default:
		s.notFound(w, r)
	}
}

func (s *Server) createSG(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID    string          `json:"clusterId"`
		Name         string          `json:"name"`
		IngressRules json.RawMessage `json:"ingressRules"`
		EgressRules  json.RawMessage `json:"egressRules"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	rec := &sgRecord{
		ID: id, ClusterID: body.ClusterID, Name: body.Name,
		UserID: "test-user", Status: "ready",
		Ingress: body.IngressRules, Egress: body.EgressRules,
	}
	s.sgMap[id] = rec
	s.writeJSON(w, http.StatusOK, sgWire(rec))
}

func (s *Server) listSGs(w http.ResponseWriter, r *http.Request) {
	want := r.URL.Query().Get("ids")
	items := []map[string]any{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sg := range s.sgMap {
		if want != "" && id != want {
			continue
		}
		items = append(items, sgWire(sg))
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

func (s *Server) handleSGBulkDelete(w http.ResponseWriter, r *http.Request) {
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
		delete(s.sgMap, id)
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// handleSGItem serves PATCH /v1/security_groups/{id}.
func (s *Server) handleSGItem(w http.ResponseWriter, r *http.Request) {
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
		Name         *string         `json:"name"`
		IngressRules json.RawMessage `json:"ingressRules"`
		EgressRules  json.RawMessage `json:"egressRules"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.sgMap[id]
	if !ok {
		s.writeError(w, "security_group not found")
		return
	}
	if body.Name != nil {
		rec.Name = *body.Name
	}
	if len(body.IngressRules) > 0 {
		rec.Ingress = body.IngressRules
	}
	if len(body.EgressRules) > 0 {
		rec.Egress = body.EgressRules
	}
	s.writeJSON(w, http.StatusOK, sgWire(rec))
}

func sgWire(rec *sgRecord) map[string]any {
	out := map[string]any{
		"id":         rec.ID,
		"clusterId":  rec.ClusterID,
		"name":       rec.Name,
		"userId":     rec.UserID,
		"status":     rec.Status,
		"vpcId":      rec.VPCID,
		"attachedTo": []string{},
		"createdAt":  "2026-01-01T00:00:00Z",
	}
	if len(rec.Ingress) > 0 {
		out["ingressRules"] = rec.Ingress
	} else {
		out["ingressRules"] = map[string]string{"type": "allowAll"}
	}
	if len(rec.Egress) > 0 {
		out["egressRules"] = rec.Egress
	} else {
		out["egressRules"] = map[string]string{"type": "allowAll"}
	}
	return out
}
