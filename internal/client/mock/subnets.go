package mock

import (
	"encoding/json"
	"net/http"
)

type subnetRecord struct {
	ID, Name, VPCID, ClusterID, UserID, Status string
	IPv4, IPv6                                 string
}

// wireSubnetsOnce is called from New() to register subnet handlers idempotently.
func (s *Server) wireSubnetsOnce() { s.subnetWiringOnce.Do(s.wireSubnets) }

func (s *Server) wireSubnets() {
	s.mu.Lock()
	if s.subnetMap == nil {
		s.subnetMap = map[string]*subnetRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v1/vpc/", func(w http.ResponseWriter, r *http.Request) {
		// Path: /v1/vpc/{vpc_id}/subnets — create.
		if r.Method != http.MethodPost {
			s.notFound(w, r)
			return
		}
		parts := splitPath(r.URL.Path)
		if len(parts) != 4 || parts[0] != "v1" || parts[1] != "vpc" || parts[3] != "subnets" {
			s.notFound(w, r)
			return
		}
		vpcID := parts[2]
		var body struct {
			ClusterID string  `json:"clusterId"`
			Name      string  `json:"name"`
			IPv4Cidr  *string `json:"ipv4Cidr,omitempty"`
			IPv6Cidr  *string `json:"ipv6Cidr,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		id := newID()
		rec := &subnetRecord{ID: id, Name: body.Name, VPCID: vpcID, ClusterID: body.ClusterID, UserID: "test-user", Status: "ready"}
		if body.IPv4Cidr != nil {
			rec.IPv4 = *body.IPv4Cidr
		}
		if body.IPv6Cidr != nil {
			rec.IPv6 = *body.IPv6Cidr
		}
		s.subnetMap[id] = rec
		s.writeJSON(w, http.StatusOK, subnetWire(rec))
	})
	s.mux.HandleFunc("/v1/subnets", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
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
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	s.mux.HandleFunc("/v1/subnets/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		for _, id := range body.IDs {
			delete(s.subnetMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
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
	}
	if rec.IPv4 != "" {
		out["ipv4Cidr"] = rec.IPv4
	}
	if rec.IPv6 != "" {
		out["ipv6Cidr"] = rec.IPv6
	}
	return out
}
