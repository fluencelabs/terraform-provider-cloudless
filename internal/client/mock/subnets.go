package mock

import (
	"encoding/json"
	"net/http"
)

type subnetRecord struct {
	ID, Name, VPCID, ClusterID, UserID, Status string
	IPv4, IPv6                                 string
	Egress                                     bool
	IsDefault                                  bool
}

// wireSubnetsOnce is called from New() to register subnet handlers idempotently.
func (s *Server) wireSubnetsOnce() { s.subnetWiringOnce.Do(s.wireSubnets) }

// The default subnet's VPC and cluster: ids only tests compare against, so
// they need to be stable, not real.
const (
	defaultSubnetVPCID     = "00000000-0000-4000-8000-0000000005a1"
	defaultSubnetClusterID = "00000000-0000-4000-8000-0000000005c1"
)

func (s *Server) wireSubnets() {
	s.mu.Lock()
	if s.subnetMap == nil {
		s.subnetMap = map[string]*subnetRecord{}
	}
	// Every account has a default subnet per cluster — the one a VM lands on
	// with no network_interface block; it is the same subnet a fresh draft's
	// default interface binds.
	if _, ok := s.subnetMap[defaultDraftSubnetID]; !ok {
		s.subnetMap[defaultDraftSubnetID] = &subnetRecord{
			ID:        defaultDraftSubnetID,
			Name:      "default",
			VPCID:     defaultSubnetVPCID,
			ClusterID: defaultSubnetClusterID,
			UserID:    "test-user",
			Status:    "ready",
			IPv4:      "10.0.0.0/24",
			Egress:    true,
			IsDefault: true,
		}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v3/subnets", s.handleSubnetCollection)
	s.mux.HandleFunc("/v3/subnets/", s.handleSubnetItem)
}

// subnetCreatePathParts is the segment count of /v3/vpcs/{vpc_id}/subnets.
const subnetCreatePathParts = 4

// handleSubnetCreate serves POST /v1/vpcs/{vpc_id}/subnets; dispatched from
// the /v1/vpcs/ handler in vpcs.go.
func (s *Server) handleSubnetCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.notFound(w, r)
		return
	}
	parts := splitPath(r.URL.Path)
	if len(parts) != subnetCreatePathParts || parts[0] != "v3" || parts[1] != "vpcs" || parts[3] != "subnets" {
		s.notFound(w, r)
		return
	}
	vpcID := parts[2]
	var body struct {
		Name     string  `json:"name"`
		Egress   *bool   `json:"egress"`
		IPv4Cidr *string `json:"ipv4Cidr,omitempty"`
		IPv6Cidr *string `json:"ipv6Cidr,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	// A subnet needs at least one CIDR; the public spec marks both nullable,
	// the API does not (observed on stage 2026-09-11).
	if body.IPv4Cidr == nil && body.IPv6Cidr == nil {
		s.writeJSON(w, http.StatusBadRequest,
			map[string]string{"error": "No one cidr provided", "code": "bad_request"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	rec := &subnetRecord{
		ID:        id,
		Name:      body.Name,
		VPCID:     vpcID,
		ClusterID: s.clusterOfVPC(vpcID),
		Egress:    body.Egress == nil || *body.Egress,
		UserID:    "test-user",
		Status:    "ready",
	}
	if body.IPv4Cidr != nil {
		rec.IPv4 = *body.IPv4Cidr
	}
	if body.IPv6Cidr != nil {
		rec.IPv6 = *body.IPv6Cidr
	}
	s.subnetMap[id] = rec
	s.writeJSON(w, http.StatusOK, subnetWire(rec))
}

func (s *Server) handleSubnetCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.notFound(w, r)
		return
	}
	want := r.URL.Query().Get("ids")
	items := []map[string]any{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sn := range s.subnetMap {
		if want != "" && id != want {
			continue
		}
		items = append(items, subnetWire(sn))
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

func subnetWire(rec *subnetRecord) map[string]any {
	out := map[string]any{
		"id":        rec.ID,
		"name":      rec.Name,
		"vpcId":     rec.VPCID,
		"clusterId": rec.ClusterID,
		"userId":    rec.UserID,
		"status":    rec.Status,
		"egress":    rec.Egress,
		"isDefault": rec.IsDefault,
	}
	if rec.IPv4 != "" {
		out["ipv4Cidr"] = rec.IPv4
	}
	if rec.IPv6 != "" {
		out["ipv6Cidr"] = rec.IPv6
	}
	return out
}

// handleSubnetItem serves PATCH and DELETE on /v3/subnets/{id}.
func (s *Server) handleSubnetItem(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) != resourcePathParts {
		s.notFound(w, r)
		return
	}
	id := parts[2]
	var body struct {
		Name   *string `json:"name"`
		Egress *bool   `json:"egress"`
	}
	if r.Method == http.MethodPatch {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.subnetMap[id]
	if !ok {
		s.writeError(w, "subnet not found")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		if body.Name != nil {
			rec.Name = *body.Name
		}
		if body.Egress != nil {
			rec.Egress = *body.Egress
		}
	case http.MethodDelete:
		wire := subnetWire(rec)
		delete(s.subnetMap, id)
		s.writeJSON(w, http.StatusOK, wire)
		return
	default:
		s.notFound(w, r)
		return
	}
	s.writeJSON(w, http.StatusOK, subnetWire(rec))
}
