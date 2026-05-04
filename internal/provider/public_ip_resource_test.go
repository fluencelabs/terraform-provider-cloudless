package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitPublicIP_Create(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_public_ip" "edge" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "edge"
  address_type = "V4"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_public_ip.edge", "address_type", "V4"),
					resource.TestCheckResourceAttr("cloudless_public_ip.edge", "status", "ready"),
					resource.TestCheckResourceAttrSet("cloudless_public_ip.edge", "address"),
				),
			},
		},
	})
}
