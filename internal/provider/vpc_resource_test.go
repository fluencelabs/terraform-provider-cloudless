package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitVPC_CreateUpdateRename(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_vpc" "main" {
  cluster_id = "92e6ed24-8bfa-4737-9409-a1aac994e1f5"
  name       = "main"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vpc.main", "name", "main"),
					resource.TestCheckResourceAttr("cloudless_vpc.main", "status", "ready"),
				),
			},
			{
				Config: `
resource "cloudless_vpc" "main" {
  cluster_id = "92e6ed24-8bfa-4737-9409-a1aac994e1f5"
  name       = "renamed"
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_vpc.main", "name", "renamed"),
			},
		},
	})
}
