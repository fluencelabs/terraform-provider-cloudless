package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitSSHKey_CreateAndRead(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_ssh_key" "me" {
  name       = "demo"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1DjqOOxINs78oU3f7PJXIyq9uiNocNVhXNx user@example.com"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						"cloudless_ssh_key.me",
						tfjsonpath.New("name"),
						knownvalue.StringExact("demo"),
					),
					statecheck.ExpectKnownValue("cloudless_ssh_key.me", tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(
						"cloudless_ssh_key.me",
						tfjsonpath.New("fingerprint"),
						knownvalue.NotNull(),
					),
				},
			},
		},
	})
}

// Fluence dedups SSH keys by key body (ignoring the comment). When the same
// key material is already registered out of band under a different name, the
// create returns 409; the provider must adopt the existing key rather than
// fail, and the adoption must be stable (no replace-loop on the next plan).
func TestUnitSSHKey_AdoptsExistingKeyOnConflict(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	const body = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIME9HKAAGtKnkZNQHQ"

	// A key with the same body, registered earlier under a different name and
	// with no comment.
	existing, err := h.Client.CreateSSHKey(h.Ctx(), client.CreateSSHKeyRequest{
		Name:      "my",
		PublicKey: body,
	})
	if err != nil {
		t.Fatalf("seed existing key: %v", err)
	}

	cfg := fmt.Sprintf(`
resource "cloudless_ssh_key" "me" {
  name       = "tf-key-1"
  public_key = %q
}
`, body+" gurinderu@gmail.com")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				ConfigStateChecks: []statecheck.StateCheck{
					// Adopted the existing key's id rather than creating a new one.
					statecheck.ExpectKnownValue(
						"cloudless_ssh_key.me",
						tfjsonpath.New("id"),
						knownvalue.StringExact(existing.ID),
					),
					// State keeps the config's name and public_key (with comment).
					statecheck.ExpectKnownValue(
						"cloudless_ssh_key.me",
						tfjsonpath.New("name"),
						knownvalue.StringExact("tf-key-1"),
					),
					statecheck.ExpectKnownValue(
						"cloudless_ssh_key.me",
						tfjsonpath.New("public_key"),
						knownvalue.StringExact(body+" gurinderu@gmail.com"),
					),
				},
				// The default post-apply refresh plan must be empty: a naive
				// adopt would surface a perpetual name/comment RequiresReplace.
			},
		},
	})
}

func TestUnitSSHKey_RenameForcesReplacement(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	cfg := func(name string) string {
		return fmt.Sprintf(`
resource "cloudless_ssh_key" "me" {
  name       = %q
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1DjqOOxINs78oU3f7PJXIyq9uiNocNVhXNx user@example.com"
}
`, name)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{Config: cfg("first")},
			{
				Config: cfg("second"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"cloudless_ssh_key.me",
							plancheck.ResourceActionDestroyBeforeCreate,
						),
					},
				},
			},
		},
	})
}
