package mock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// sshKeyRecord is the mock's in-memory shape for an SSH key.
type sshKeyRecord struct {
	// Fingerprint is SYNTHETIC for tests: hex-truncated sha256 of the public-key text,
	// not the RFC-4253 base64 fingerprint of the binary key blob. Don't assert on its format.
	ID, UserID, Name, PublicKey, Algorithm, Fingerprint string
}

// wireSSHKeysOnce is called from New() to register SSH key handlers
// idempotently. Per the defensive-lock pattern: do NOT hold s.mu around
// the sync.Once.Do call; wireSSHKeys locks briefly only for map init,
// then performs mutex-free mux registration.
func (s *Server) wireSSHKeysOnce() { s.sshKeyWiring.Do(s.wireSSHKeys) }

func (s *Server) wireSSHKeys() {
	s.mu.Lock()
	if s.sshKeyMap == nil {
		s.sshKeyMap = map[string]*sshKeyRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v3/ssh-keys", s.handleSSHKeysCollection)
	s.mux.HandleFunc("/v3/ssh-keys/", s.handleSSHKeyItem)
}

func (s *Server) handleSSHKeysCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createSSHKey(w, r)
	case http.MethodGet:
		s.listSSHKeys(w, r)
	default:
		s.notFound(w, r)
	}
}

func (s *Server) createSSHKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		PublicKey string `json:"publicKey"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Fluence dedups by key body (ignoring the comment): a second registration
	// of the same material returns 409.
	for _, k := range s.sshKeyMap {
		if sameSSHKeyBody(k.PublicKey, body.PublicKey) {
			s.writeJSON(w, http.StatusConflict, map[string]string{"error": "SshKey already exists", "code": "conflict"})
			return
		}
	}
	id := newID()
	sum := sha256.Sum256([]byte(body.PublicKey))
	rec := &sshKeyRecord{
		ID:          id,
		UserID:      "test-user",
		Name:        body.Name,
		PublicKey:   body.PublicKey,
		Algorithm:   "ssh-ed25519",
		Fingerprint: "SHA256:" + hex.EncodeToString(sum[:8]),
	}
	s.sshKeyMap[id] = rec
	s.writeJSON(w, http.StatusCreated, sshKeyWire(rec))
}

func (s *Server) listSSHKeys(w http.ResponseWriter, r *http.Request) {
	want := r.URL.Query().Get("ids")
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []map[string]any{}
	for id, k := range s.sshKeyMap {
		if want != "" && id != want {
			continue
		}
		items = append(items, sshKeyWire(k))
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

// sameSSHKeyBody compares two OpenSSH public keys by algorithm + base64 body,
// ignoring any trailing comment — mirroring how the real API dedups keys.
func sameSSHKeyBody(a, b string) bool {
	fa := strings.Fields(a)
	fb := strings.Fields(b)
	if len(fa) < 2 || len(fb) < 2 {
		return false
	}
	return fa[0] == fb[0] && fa[1] == fb[1]
}

func sshKeyWire(rec *sshKeyRecord) map[string]any {
	return map[string]any{
		"id":          rec.ID,
		"name":        rec.Name,
		"publicKey":   rec.PublicKey,
		"algorithm":   rec.Algorithm,
		"fingerprint": rec.Fingerprint,
	}
}

// handleSSHKeyItem serves DELETE /v3/ssh-keys/{id}.
func (s *Server) handleSSHKeyItem(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) != resourcePathParts || r.Method != http.MethodDelete {
		s.notFound(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.sshKeyMap[parts[2]]
	if !ok {
		s.writeError(w, "ssh key not found")
		return
	}
	wire := sshKeyWire(rec)
	delete(s.sshKeyMap, parts[2])
	s.writeJSON(w, http.StatusOK, wire)
}
