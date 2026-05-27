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

const (
	// resourcePathParts is the segment count of a "/v{n}/{resource}/{id}" path
	// once split (e.g. ["v1", "public_ips", "<id>"]).
	resourcePathParts = 3
	// defaultPerPage is the page size echoed in mock list responses.
	defaultPerPage = 100
	// idByteLen is the number of random bytes backing a generated mock ID.
	idByteLen = 16
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
	// publicIPSeq assigns sequential octets to synthesized public IP
	// addresses. Guarded by s.mu.
	publicIPSeq  uint32
	vmMap        map[string]*vmRecord
	vmWiring     sync.Once
	sshKeyMap    map[string]*sshKeyRecord
	sshKeyWiring sync.Once

	// reqBodyValidator validates incoming request bodies against the vendored
	// public OpenAPI spec; contractEnforce toggles that enforcement (default
	// on). SetContractEnforcement lets a test that deliberately sends a
	// malformed body opt out.
	reqBodyValidator  requestBodyValidator
	respBodyValidator responseBodyValidator
	contractEnforce   bool
	// contractViolations records response-shape drift (the mock returning a body
	// the spec doesn't allow). Guarded by s.mu. Tests assert it stays empty.
	contractViolations []string

	// FailRemoveVMStorages, when set, makes /v2/vms/{id}/storages/remove return 500.
	FailRemoveVMStorages bool

	// restartCount counts VM restart/softreboot calls. Guarded by s.mu; read via
	// RestartCount so tests can assert a restart actually happened.
	restartCount int
}

// RestartCount returns how many times a VM restart/softreboot endpoint has been
// called against this mock.
func (s *Server) RestartCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restartCount
}

// SetContractEnforcement toggles OpenAPI request-body validation in the mock.
// On by default; disable it for tests that intentionally send a body the spec
// rejects.
func (s *Server) SetContractEnforcement(on bool) { s.contractEnforce = on }

// ContractViolations returns response-contract violations observed so far (the
// mock's responses drifting from the spec). Empty means the mock's responses
// conform to the vendored public API.
func (s *Server) ContractViolations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.contractViolations...)
}

// New starts a server and returns it. Callers must defer s.Close().
func New() *Server {
	s := &Server{mux: http.NewServeMux(), contractEnforce: true}
	s.register(s.mux)
	reqV, _, reqErr := NewPublicAPIRequestBodyValidator()
	respV, _, respErr := NewPublicAPIResponseBodyValidator()
	if reqErr != nil || respErr != nil {
		// A broken vendored spec is a test-infra error, not a runtime path.
		panic("mock: cannot build OpenAPI contract validators")
	}
	s.reqBodyValidator = reqV
	s.respBodyValidator = respV
	s.Server = httptest.NewServer(s.contractMiddleware(s.mux))
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

// writeError writes a Fluence ErrorBody-shaped 404 JSON response.
func (s *Server) writeError(w http.ResponseWriter, msg string) {
	s.writeJSON(w, http.StatusNotFound, map[string]string{"error": msg})
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
	b := make([]byte, idByteLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(
		b[:4],
	) + "-" + hex.EncodeToString(
		b[4:6],
	) + "-" + hex.EncodeToString(
		b[6:8],
	) + "-" + hex.EncodeToString(
		b[8:10],
	) + "-" + hex.EncodeToString(
		b[10:16],
	)
}
