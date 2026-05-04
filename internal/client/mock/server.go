// Package mock provides an in-memory implementation of the Fluence public API
// for use in unit tests. It exposes a *httptest.Server so tests can point a
// real *client.Client at it and exercise full HTTP/JSON paths.
package mock

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Server wraps an httptest.Server and holds the in-memory state of the
// resources it knows how to serve. Concrete endpoints are registered by the
// per-resource files in this package; the mux is kept reachable so each
// resource's wireXOnce helper can register handlers idempotently.
type Server struct {
	*httptest.Server
	mu  sync.Mutex
	mux *http.ServeMux

	// Per-resource state. Each resource type adds its own field as it lands.
	// All maps are guarded by s.mu.
	vpcMap            map[string]*vpcRecord
	vpcWiringOnce     sync.Once
	subnetMap         map[string]*subnetRecord
	subnetWiringOnce  sync.Once
	clusterMap        map[string]map[string]any
	clustersWiring    sync.Once
	dcMap             map[string]map[string]any
	datacentersWiring sync.Once
	sgMap             map[string]*sgRecord
	sgWiring          sync.Once
	storageMap        map[string]*storageRecord
	storageWiring     sync.Once
	publicIPMap       map[string]*publicIPRecord
	publicIPWiring    sync.Once
	vmMap             map[string]*vmRecord
	vmWiring          sync.Once
	sshKeyMap         map[string]*sshKeyRecord
	sshKeyWiring      sync.Once

	// FailRemoveVMStorages, when set, makes /v2/vms/{id}/storages/remove return 500.
	FailRemoveVMStorages bool
}

// New starts a server and returns it. Callers must defer s.Close().
func New() *Server {
	s := &Server{mux: http.NewServeMux()}
	s.register(s.mux)
	s.Server = httptest.NewServer(s.mux)
	s.wireVPCsOnce()
	s.wireSubnetsOnce()
	s.wireClustersOnce()
	s.wireDCsOnce()
	s.wireSGsOnce()
	s.wireStoragesOnce()
	s.wirePublicIPsOnce()
	s.wireVMsOnce()
	s.wireSSHKeysOnce()
	return s
}

// register wires the catch-all 404 handler. Resource-specific endpoints are
// wired by their wireXOnce helpers, called from New().
func (s *Server) register(mux *http.ServeMux) {
	// Catch-all 404 with a JSON ErrorBody so client error decoding works.
	mux.HandleFunc("/", s.notFound)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "no route: " + r.Method + " " + r.URL.Path})
}

// writeJSON is a tiny helper used by every concrete handler.
func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError writes a Fluence ErrorBody-shaped JSON response.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

// splitPath splits "/a/b/c" → ["a","b","c"].
func splitPath(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, v := range parts {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
}
