package mock

import (
	"net/http"
)

// wireClustersOnce is called from New() to register cluster handlers
// idempotently. Per the defensive-lock pattern: do NOT hold s.mu around
// the sync.Once.Do call; wireClusters locks briefly only for map init,
// then performs mutex-free mux registration.
func (s *Server) wireClustersOnce() { s.clustersWiring.Do(s.wireClusters) }

func (s *Server) wireClusters() {
	s.mu.Lock()
	if s.clusterMap == nil {
		s.clusterMap = map[string]map[string]any{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v1/clusters", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for _, c := range s.clusterMap {
			out = append(out, c)
		}
		s.writeJSON(w, http.StatusOK, out)
	})
}

// wireDCsOnce is called from New() to register datacenter handlers
// idempotently. Per the defensive-lock pattern: do NOT hold s.mu around
// the sync.Once.Do call; wireDCs locks briefly only for map init, then
// performs mutex-free mux registration.
func (s *Server) wireDCsOnce() { s.datacentersWiring.Do(s.wireDCs) }

func (s *Server) wireDCs() {
	s.mu.Lock()
	if s.dcMap == nil {
		s.dcMap = map[string]map[string]any{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v1/datacenters", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for _, d := range s.dcMap {
			out = append(out, d)
		}
		s.writeJSON(w, http.StatusOK, out)
	})
}

// SeedCluster adds a cluster (with a synthetic dc_id reference). Tests that
// also want country/city data should call SeedDatacenter() to register the
// referenced datacenter.
func (s *Server) SeedCluster(id, name, dcID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clusterMap == nil {
		s.clusterMap = map[string]map[string]any{}
	}
	s.clusterMap[id] = map[string]any{"id": id, "name": name, "dc_id": dcID}
}

// mockDatacenterTier is the fixed tier value seeded for mock datacenter rows.
const mockDatacenterTier = 3

// SeedDatacenter registers a datacenter row.
func (s *Server) SeedDatacenter(id, country, city, slug string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dcMap == nil {
		s.dcMap = map[string]map[string]any{}
	}
	s.dcMap[id] = map[string]any{
		"id": id, "countryCode": country, "cityCode": city,
		"index": 0, "tier": mockDatacenterTier, "certifications": []string{}, "slug": slug,
	}
}
