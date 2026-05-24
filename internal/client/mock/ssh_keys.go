package mock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
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

	s.mux.HandleFunc("/v1/ssh_keys", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Name      string `json:"name"`
				PublicKey string `json:"publicKey"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
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
			s.writeJSON(w, http.StatusOK, sshKeyWire(rec))
		case http.MethodGet:
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
		default:
			s.notFound(w, r)
		}
	})
	// /v1/ssh_keys/delete is an exact path; ServeMux prefers exact matches over
	// any prefix, so register it explicitly.
	s.mux.HandleFunc("/v1/ssh_keys/delete", func(w http.ResponseWriter, r *http.Request) {
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
			delete(s.sshKeyMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

func sshKeyWire(rec *sshKeyRecord) map[string]any {
	return map[string]any{
		"id":          rec.ID,
		"userId":      rec.UserID,
		"name":        rec.Name,
		"publicKey":   rec.PublicKey,
		"algorithm":   rec.Algorithm,
		"fingerprint": rec.Fingerprint,
	}
}
