package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitSubnet_DerivesClusterIDFromVPC(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	h.Mock.SeedVPC("99999999-9999-4999-8999-999999999999", "main", "cluster-X")
	// (Subnet endpoints are auto-wired by mock.New(); no extra seeding needed.)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_subnet" "s" {
  vpc_id = "99999999-9999-4999-8999-999999999999"
  name   = "demo"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_subnet.s", "cluster_id", "cluster-X"),
				),
			},
		},
	})
}
