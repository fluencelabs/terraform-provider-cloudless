package client_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi-validator/requests"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

// newContractValidator builds a request/response validator from the vendored
// public OpenAPI spec (api.fluence.dev). This is the authoritative contract;
// drift between what the provider sends and what the API accepts shows up here
// without a live account.
func newContractValidator(t *testing.T) requests.RequestBodyValidator {
	t.Helper()
	v, warnings, err := mock.NewPublicAPIRequestBodyValidator()
	if err != nil {
		t.Fatalf("build validator: %v", err)
	}
	for _, w := range warnings {
		t.Logf("spec build warning (tolerated): %v", w)
	}
	return v
}

// validateBody POSTs body to path through the validator and reports whether the
// request conforms to the spec.
func validateBody(
	t *testing.T,
	v requests.RequestBodyValidator,
	path string,
	body any,
) (bool, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	ok, valErrs := v.ValidateRequestBody(req)
	var msg strings.Builder
	for _, e := range valErrs {
		msg.WriteString(e.Message)
		msg.WriteString("; ")
	}
	return ok, msg.String()
}

func TestContract_SGCreate_AllowListedArray(t *testing.T) {
	v := newContractValidator(t)
	ok, msg := validateBody(t, v, "/v1/security_groups", client.CreateSecurityGroupRequest{
		ClusterID: "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		Name:      "web",
		IngressRules: client.RulesToCreateField(client.SecurityGroupRules{
			Mode:  "allow",
			Rules: []client.SecurityGroupRule{sampleRule()},
		}),
		EgressRules: client.RulesToCreateField(client.SecurityGroupRules{Mode: "allowAll"}),
	})
	if !ok {
		t.Fatalf("allow-listed SG create should satisfy the contract: %s", msg)
	}
}

func TestContract_SGCreate_AllowAllOmitsFields(t *testing.T) {
	v := newContractValidator(t)
	ok, msg := validateBody(t, v, "/v1/security_groups", client.CreateSecurityGroupRequest{
		ClusterID:    "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		Name:         "web",
		IngressRules: client.RulesToCreateField(client.SecurityGroupRules{Mode: "allowAll"}),
		EgressRules:  client.RulesToCreateField(client.SecurityGroupRules{Mode: "allowAll"}),
	})
	if !ok {
		t.Fatalf("allow-all (omitted rule fields) should satisfy the contract: %s", msg)
	}
}

// The pre-fix object form {"type":"allow","rules":[...]} must be rejected by the
// contract — this is the exact regression that reached a live apply as
// "400 ingressRules: invalid type: map, expected a sequence".
func TestContract_SGCreate_ObjectFormRejected(t *testing.T) {
	v := newContractValidator(t)
	objectForm := map[string]any{
		"clusterId":    "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		"name":         "web",
		"ingressRules": map[string]any{"type": "allow", "rules": []any{}},
		"egressRules":  map[string]any{"type": "allowAll"},
	}
	ok, _ := validateBody(t, v, "/v1/security_groups", objectForm)
	if ok {
		t.Fatalf("object-form ingressRules must violate the contract (API wants an array)")
	}
}

func TestContract_SGCreate_DenyAllEmptyArray(t *testing.T) {
	v := newContractValidator(t)
	ok, msg := validateBody(t, v, "/v1/security_groups", client.CreateSecurityGroupRequest{
		ClusterID: "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		Name:      "locked",
		IngressRules: client.RulesToCreateField(client.SecurityGroupRules{
			Mode:  "allow",
			Rules: []client.SecurityGroupRule{},
		}),
		EgressRules: client.RulesToCreateField(client.SecurityGroupRules{Mode: "allowAll"}),
	})
	if !ok {
		t.Fatalf("deny-all (empty array) should satisfy the contract: %s", msg)
	}
}

func TestContract_SSHKeyCreate(t *testing.T) {
	v := newContractValidator(t)
	ok, msg := validateBody(t, v, "/v1/ssh_keys", client.CreateSSHKeyRequest{
		Name:      "my-key",
		PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1Djq u@e",
	})
	if !ok {
		t.Fatalf("ssh key create should satisfy the contract: %s", msg)
	}
}

func TestContract_StorageCreate(t *testing.T) {
	v := newContractValidator(t)
	ok, msg := validateBody(t, v, "/v1/storages", client.CreateStorageRequest{
		ClusterID:   "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		Name:        "data-vol",
		StorageType: "NVME",
		VolumeGb:    40,
		Replicated:  true,
	})
	if !ok {
		t.Fatalf("storage create should satisfy the contract: %s", msg)
	}
}

// Pins the prior regression (commit 07d900c): an inline boot disk is a
// CreateUserStorageRequest and MUST carry clusterId, or the API rejects the VM
// create with "bootDisk: data did not match any variant of untagged enum".
func TestContract_VMCreate_InlineBootDiskCarriesClusterID(t *testing.T) {
	v := newContractValidator(t)
	ok, msg := validateBody(t, v, "/v2/vms", client.CreateVMRequest{
		ClusterID:       "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		Name:            "vm",
		ConfigurationID: "5e864016-9d07-46c9-a485-bc11ad778f3b",
		BootDisk: client.VMBootDisk{Create: &client.CreateUserStorageInline{
			ClusterID:   "3fa85f64-5717-4562-b3fc-2c963f66afa6",
			Name:        "vm-boot",
			StorageType: "NVME",
			VolumeGb:    40,
			Replicated:  true,
		}},
	})
	if !ok {
		t.Fatalf("VM create with inline boot disk (clusterId present) should conform: %s", msg)
	}
}

func TestContract_VMCreate_InlineBootDiskWithoutClusterIDRejected(t *testing.T) {
	v := newContractValidator(t)
	body := map[string]any{
		"clusterId":       "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		"name":            "vm",
		"configurationId": "5e864016-9d07-46c9-a485-bc11ad778f3b",
		"bootDisk": map[string]any{
			// clusterId intentionally omitted — the regression.
			"name":        "vm-boot",
			"storageType": "NVME",
			"volumeGb":    40,
			"replicated":  true,
		},
	}
	ok, _ := validateBody(t, v, "/v2/vms", body)
	if ok {
		t.Fatalf("inline boot disk without clusterId must violate the contract")
	}
}
