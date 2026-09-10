package mock_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

const probeClusterID = "3fa85f64-5717-4562-b3fc-2c963f66afa6"

func postJSON(t *testing.T, url, body string) int {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// The mock enforces the vendored OpenAPI contract on requests: the pre-fix
// object form for ingressRules must be rejected with 400, exactly as the real
// API does ("invalid type: map, expected a sequence").
func TestContractMiddleware_RejectsObjectFormSGCreate(t *testing.T) {
	s := mock.New()
	defer s.Close()
	code := postJSON(t, s.URL+"/v1/security_groups", `{
		"clusterId": "`+probeClusterID+`",
		"name": "x",
		"ingressRules": {"type": "allow", "rules": []},
		"egressRules": {"type": "allowAll"}
	}`)
	if code != http.StatusBadRequest {
		t.Fatalf("object-form SG create: got HTTP %d, want 400", code)
	}
}

func TestContractMiddleware_AcceptsArraySGCreate(t *testing.T) {
	s := mock.New()
	defer s.Close()
	rule := `{"type":"ipv4","protocolKind":{"tcp":{"ports":{"exact":{"value":22}}}},` +
		`"remote":{"address":"0.0.0.0/0"}}`
	body := `{"clusterId":"` + probeClusterID + `","name":"x",` +
		`"ingressRules":[` + rule + `],"egressRules":null}`
	if code := postJSON(t, s.URL+"/v1/security_groups", body); code == http.StatusBadRequest {
		t.Fatalf("array-form SG create should pass the contract, got 400")
	}
}

// The mock's own responses must conform to the spec, so the provider's read
// path is tested against the real response shape. Exercise representative
// create flows and assert no response-contract drift was recorded.
func TestContractMiddleware_MockResponsesConform(t *testing.T) {
	s := mock.New()
	defer s.Close()

	cid := probeClusterID
	sgRule := `[{"type":"ipv4","protocolKind":{"tcp":{"ports":{"exact":{"value":22}}}},` +
		`"remote":{"address":"0.0.0.0/0"}}]`
	posts := []struct {
		path, body string
	}{
		{"/v1/ssh_keys", `{"name":"k","publicKey":"ssh-ed25519 AAAA u@e"}`},
		{"/v1/vpcs", `{"clusterId":"` + cid + `","name":"vpc"}`},
		{"/v1/storages", `{"clusterId":"` + cid + `","name":"vol",` +
			`"storageType":"NVME","volumeGb":40,"replicated":true}`},
		{"/v1/public_ips", `{"clusterId":"` + cid + `","name":"ip","addressType":"V4"}`},
		{"/v1/security_groups", `{"clusterId":"` + cid + `","name":"sg",` +
			`"ingressRules":` + sgRule + `,"egressRules":null}`},
	}
	for _, p := range posts {
		if code := postJSON(t, s.URL+p.path, p.body); code >= 500 {
			t.Fatalf("POST %s returned %d", p.path, code)
		}
	}

	if v := s.ContractViolations(); len(v) > 0 {
		t.Fatalf("mock responses drifted from the spec:\n  %s", strings.Join(v, "\n  "))
	}
}
