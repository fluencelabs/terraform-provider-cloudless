package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

// The account's default subnet is discoverable, so a configuration can point
// at it instead of creating a VPC and a subnet of its own.
func TestUnitSubnets_FindsTheDefault(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_subnets" "all" {}

output "default_subnet" {
  value = [for s in data.cloudless_subnets.all.subnets : s.id if s.is_default][0]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudless_subnets.all", "subnets.#", "1"),
					resource.TestCheckResourceAttr("data.cloudless_subnets.all", "subnets.0.is_default", "true"),
					resource.TestCheckResourceAttr("data.cloudless_subnets.all", "subnets.0.egress", "true"),
					resource.TestCheckResourceAttrSet("data.cloudless_subnets.all", "subnets.0.ipv4_cidr"),
				),
			},
		},
	})
}
