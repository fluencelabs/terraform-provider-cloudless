package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func readRaw(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }

// VM network interfaces, shared by the /v2 read/patch handlers and the /v3
// draft/live mutation handlers. Mirrors the API's interface model: private
// (subnet, static IPs) or public (public IP), one default per VM, security
// group per interface.

// defaultDraftSubnetID is the subnet a fresh draft's default interface binds
// to, mirroring the server picking the VPC default subnet.
const defaultDraftSubnetID = "00000000-0000-4000-8000-0000000005b1"

// interfacesVerb is the /interfaces path segment shared by /v2 and /v3.
const interfacesVerb = "interfaces"

type vmInterfaceRecord struct {
	ID              string
	Default         bool
	Subnet          string // private interface; "" = unbound (draft only)
	PublicIP        string // public interface
	SecurityGroupID *string
	StaticIPs       []string
}

func newDefaultInterface() vmInterfaceRecord {
	return vmInterfaceRecord{ID: newID(), Default: true, Subnet: defaultDraftSubnetID, StaticIPs: []string{}}
}

func interfaceWire(ni vmInterfaceRecord) map[string]any {
	m := map[string]any{
		"id":          ni.ID,
		"default":     ni.Default,
		"staticIps":   ni.StaticIPs,
		"assignedIps": []string{},
	}
	if ni.StaticIPs == nil {
		m["staticIps"] = []string{}
	}
	if ni.PublicIP != "" {
		m["kind"] = map[string]any{"public": map[string]any{"public_ip": ni.PublicIP}}
	} else {
		var subnet any
		if ni.Subnet != "" {
			subnet = ni.Subnet
		}
		m["kind"] = map[string]any{"private": map[string]any{"subnet": subnet}}
	}
	if ni.SecurityGroupID != nil {
		m["securityGroup"] = *ni.SecurityGroupID
	}
	return m
}

// GET /v2/vms/{id}/interfaces.
func (s *Server) listVMInterfaces(w http.ResponseWriter, rec *vmRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(rec.Interfaces))
	for _, ni := range rec.Interfaces {
		out = append(out, interfaceWire(ni))
	}
	s.writeJSON(w, http.StatusOK, out)
}

// PATCH /v2/vms/{id}/interfaces/{iface}: securityGroupId and/or staticIps.
func (s *Server) patchVMInterface(w http.ResponseWriter, r *http.Request, rec *vmRecord, interfaceID string) {
	var body struct {
		SecurityGroupID *string  `json:"securityGroupId"`
		StaticIPs       []string `json:"staticIps"`
	}
	raw, _ := readRaw(r)
	_ = json.Unmarshal(raw, &body)
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range rec.Interfaces {
		if rec.Interfaces[i].ID != interfaceID {
			continue
		}
		if _, ok := keys["securityGroupId"]; ok {
			rec.Interfaces[i].SecurityGroupID = body.SecurityGroupID
			if rec.Status != draftStatus {
				// Binding changes on a live VM take effect after a restart.
				rec.RestartRequired = true
			}
		}
		if _, ok := keys["staticIps"]; ok {
			if rec.Status != draftStatus {
				s.writeJSON(
					w,
					http.StatusUnprocessableEntity,
					map[string]string{"error": "static IPs are draft-only", "code": "vm_not_draft"},
				)
				return
			}
			rec.Interfaces[i].StaticIPs = body.StaticIPs
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	s.writeError(w, "interface not found")
}

// POST /v3/vms/{id}/interfaces.
func (s *Server) addVMInterface(w http.ResponseWriter, r *http.Request, rec *vmRecord) {
	var body struct {
		Kind struct {
			Private *struct {
				Subnet string `json:"subnet"`
			} `json:"private"`
			Public *struct {
				PublicIP    *string `json:"publicIp"`
				AddressType *string `json:"addressType"`
			} `json:"public"`
		} `json:"kind"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	ni := vmInterfaceRecord{ID: newID(), StaticIPs: []string{}}
	switch {
	case body.Kind.Private != nil:
		ni.Subnet = body.Kind.Private.Subnet
	case body.Kind.Public != nil && body.Kind.Public.PublicIP != nil:
		if rec.Status == draftStatus {
			s.writeJSON(
				w,
				http.StatusUnprocessableEntity,
				map[string]string{"error": "a draft creates its own public IP", "code": "unprocessable_entity"},
			)
			return
		}
		ni.PublicIP = *body.Kind.Public.PublicIP
		// Observed on stage: attaching a reserved IP to a live VM makes the
		// public interface the default one; restart_required stays false.
		for i := range rec.Interfaces {
			rec.Interfaces[i].Default = false
		}
		ni.Default = true
		if ip, ok := s.publicIPMap[ni.PublicIP]; ok {
			if ip.AttachedTo != "" && ip.AttachedTo != rec.ID {
				// Observed on stage 0.11.1: "409 Conflict: public IP … is
				// already attached to a VM".
				s.writeJSON(w, http.StatusConflict,
					map[string]string{"error": "public IP is already attached to a VM", "code": "conflict"})
				return
			}
			ip.AttachedTo = rec.ID
		}
	case body.Kind.Public != nil:
		if rec.Status != draftStatus {
			s.writeJSON(
				w,
				http.StatusUnprocessableEntity,
				map[string]string{"error": "a live VM attaches an existing public IP", "code": "vm_not_draft"},
			)
			return
		}
		ni.PublicIP = s.newDraftPublicIP(rec)
	default:
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind is required", "code": "bad_request"})
		return
	}
	rec.Interfaces = append(rec.Interfaces, ni)
	s.writeJSON(w, http.StatusOK, interfaceWire(ni))
}

// newDraftPublicIP creates a draft public IP owned by the draft VM. Caller
// holds s.mu.
func (s *Server) newDraftPublicIP(rec *vmRecord) string {
	if s.publicIPMap == nil {
		s.publicIPMap = map[string]*publicIPRecord{}
	}
	id := newID()
	s.publicIPSeq++
	s.publicIPMap[id] = &publicIPRecord{
		ID: id, ClusterID: rec.ClusterID, Name: rec.Name + "-ip", AddressType: "V4",
		UserID: "test-user", Status: draftStatus,
		Address: fmt.Sprintf("203.0.113.%d", s.publicIPSeq%lastOctetMod),
	}
	return id
}

// DELETE /v3/vms/{id}/interfaces/{iface}.
func (s *Server) removeVMInterface(w http.ResponseWriter, rec *vmRecord, interfaceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ni := range rec.Interfaces {
		if ni.ID != interfaceID {
			continue
		}
		if ni.Default && ni.PublicIP == "" {
			s.writeJSON(
				w,
				http.StatusUnprocessableEntity,
				map[string]string{"error": "the default interface must stay", "code": "unprocessable_entity"},
			)
			return
		}
		rec.Interfaces = append(rec.Interfaces[:i], rec.Interfaces[i+1:]...)
		if ni.Default {
			// The default flag returns to the first private interface.
			for j := range rec.Interfaces {
				if rec.Interfaces[j].PublicIP == "" {
					rec.Interfaces[j].Default = true
					break
				}
			}
		}
		if ip, ok := s.publicIPMap[ni.PublicIP]; ok && ip.AttachedTo == rec.ID {
			ip.AttachedTo = ""
		}
		if rec.Status != draftStatus {
			rec.RestartRequired = true
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.writeError(w, "interface not found")
}

// PATCH /v3/vms/{id}/interfaces/{iface}: {subnet} repoints a private
// interface; {kind} converts a draft interface.
func (s *Server) updateVMInterfaceV3(w http.ResponseWriter, r *http.Request, rec *vmRecord, interfaceID string) {
	var body struct {
		Subnet *string `json:"subnet"`
		Kind   *struct {
			Type   string  `json:"type"`
			Subnet *string `json:"subnet"`
		} `json:"kind"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range rec.Interfaces {
		ni := &rec.Interfaces[i]
		if ni.ID != interfaceID {
			continue
		}
		switch {
		case body.Subnet != nil:
			if ni.PublicIP != "" {
				s.writeJSON(
					w,
					http.StatusUnprocessableEntity,
					map[string]string{"error": "not a private interface", "code": "unprocessable_entity"},
				)
				return
			}
			ni.Subnet = *body.Subnet
		case body.Kind != nil && rec.Status != draftStatus:
			s.writeJSON(
				w,
				http.StatusUnprocessableEntity,
				map[string]string{"error": "conversion is draft-only", "code": "conversion_draft_only"},
			)
			return
		case body.Kind != nil && body.Kind.Type == "public":
			ni.Subnet = ""
			ni.PublicIP = s.newDraftPublicIP(rec)
		case body.Kind != nil && body.Kind.Type == "private" && body.Kind.Subnet != nil:
			ni.PublicIP = ""
			ni.Subnet = *body.Kind.Subnet
		default:
			s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown body", "code": "bad_request"})
			return
		}
		s.writeJSON(w, http.StatusOK, interfaceWire(*ni))
		return
	}
	s.writeError(w, "interface not found")
}

// POST /v3/vms/{id}/interfaces/{iface}/default.
func (s *Server) setDefaultVMInterface(w http.ResponseWriter, rec *vmRecord, interfaceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i := range rec.Interfaces {
		if rec.Interfaces[i].ID == interfaceID {
			found = true
		}
	}
	if !found {
		s.writeError(w, "interface not found")
		return
	}
	out := make([]map[string]any, 0, len(rec.Interfaces))
	for i := range rec.Interfaces {
		rec.Interfaces[i].Default = rec.Interfaces[i].ID == interfaceID
		out = append(out, interfaceWire(rec.Interfaces[i]))
	}
	s.writeJSON(w, http.StatusOK, out)
}

// ReverseInterfaces reverses every VM's interface order, simulating the API
// listing interfaces in a different order than they were created.
func (s *Server) ReverseInterfaces() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.vmMap {
		for i, j := 0, len(rec.Interfaces)-1; i < j; i, j = i+1, j-1 {
			rec.Interfaces[i], rec.Interfaces[j] = rec.Interfaces[j], rec.Interfaces[i]
		}
	}
}
