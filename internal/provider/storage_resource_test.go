package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitStorage_CreateAndResize(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "data" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_storage.data", "volume_gb", "100"),
					resource.TestCheckResourceAttr("cloudless_storage.data", "status", "ready"),
					resource.TestCheckResourceAttr("cloudless_storage.data", "role", "DATA"),
				),
			},
			{
				Config: `
resource "cloudless_storage" "data" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data-renamed"
  storage_type = "NVME"
  volume_gb    = 200
  replicated   = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_storage.data", "name", "data-renamed"),
					resource.TestCheckResourceAttr("cloudless_storage.data", "volume_gb", "200"),
				),
			},
		},
	})
}
