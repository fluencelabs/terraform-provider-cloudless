package client_test

import (
	"encoding/json"
	"testing"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

// The create endpoint (CreateUserSecurityGroupRequest in vodopad) models each
// direction as Option<Vec<SecurityGroupRuleDto>>: an array, or absent.
//   - absent / null -> AllowAll
//   - []            -> deny-all (Allow{rules:[]})
//   - [rule, ...]   -> allow-listed
// This differs from the response/update shape ({"type":"allow","rules":[...]}),
// which is exercised elsewhere. These tests pin the request serialization.

func sampleRule() client.SecurityGroupRule {
	p := uint16(22)
	cidr := "0.0.0.0/0"
	return client.SecurityGroupRule{
		Type:         "ipv4",
		ProtocolKind: client.ProtocolKind{Kind: "tcp", Ports: client.Ports{Exact: &p}},
		Remote:       client.SGRemote{Address: &cidr},
	}
}

// marshalIngressField marshals a create request whose ingress uses the given
// rules (egress fixed to allow-all) and reports whether the ingressRules field
// is present and its raw JSON.
func marshalIngressField(t *testing.T, rules client.SecurityGroupRules) (bool, json.RawMessage) {
	t.Helper()
	req := client.CreateSecurityGroupRequest{
		ClusterID:    "11111111-1111-1111-1111-111111111111",
		Name:         "sg",
		IngressRules: client.RulesToCreateField(rules),
		EgressRules:  client.RulesToCreateField(client.SecurityGroupRules{Mode: "allowAll"}),
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if uerr := json.Unmarshal(b, &m); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	v, ok := m["ingressRules"]
	return ok, v
}

func TestCreateSecurityGroupRequest_AllowAllOmitsField(t *testing.T) {
	present, _ := marshalIngressField(t, client.SecurityGroupRules{Mode: "allowAll"})
	if present {
		t.Fatalf("allow-all must omit ingressRules entirely, but the field was present")
	}
}

func TestCreateSecurityGroupRequest_DenyAllIsEmptyArray(t *testing.T) {
	present, raw := marshalIngressField(
		t,
		client.SecurityGroupRules{Mode: "allow", Rules: []client.SecurityGroupRule{}},
	)
	if !present {
		t.Fatalf("deny-all must send ingressRules as []")
	}
	if string(raw) != "[]" {
		t.Fatalf("deny-all ingressRules = %s, want []", raw)
	}
}

func TestCreateSecurityGroupRequest_AllowListedIsArray(t *testing.T) {
	present, raw := marshalIngressField(
		t,
		client.SecurityGroupRules{Mode: "allow", Rules: []client.SecurityGroupRule{sampleRule()}},
	)
	if !present {
		t.Fatalf("allow-listed must send ingressRules as an array")
	}
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("ingressRules is not a JSON array: %s (%v)", raw, err)
	}
	if len(arr) != 1 {
		t.Fatalf("got %d rules, want 1", len(arr))
	}
	if string(arr[0]["type"]) != `"ipv4"` {
		t.Fatalf("rule type = %s, want \"ipv4\"", arr[0]["type"])
	}
	if _, ok := arr[0]["protocolKind"]; !ok {
		t.Fatalf("rule missing protocolKind: %s", raw)
	}
	if _, ok := arr[0]["remote"]; !ok {
		t.Fatalf("rule missing remote: %s", raw)
	}
}
