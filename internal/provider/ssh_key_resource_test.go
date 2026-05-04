package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

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
					statecheck.ExpectKnownValue("cloudless_ssh_key.me", tfjsonpath.New("name"), knownvalue.StringExact("demo")),
					statecheck.ExpectKnownValue("cloudless_ssh_key.me", tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue("cloudless_ssh_key.me", tfjsonpath.New("fingerprint"), knownvalue.NotNull()),
				},
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
						plancheck.ExpectResourceAction("cloudless_ssh_key.me", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})
}
