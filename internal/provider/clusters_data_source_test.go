package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitClustersDataSource_FilterRegions(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	h.Mock.SeedDatacenter("dc-de", "DE", "FRA", "fra-1")
	h.Mock.SeedDatacenter("dc-pl", "PL", "WAW", "waw-1")
	h.Mock.SeedDatacenter("dc-us", "US", "JFK", "jfk-1")
	h.Mock.SeedCluster("c-de", "DE-1", "dc-de")
	h.Mock.SeedCluster("c-pl", "PL-1", "dc-pl")
	h.Mock.SeedCluster("c-us", "US-1", "dc-us")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_clusters" "eu" {
  regions = ["DE", "PL"]
}
output "count" { value = length(data.cloudless_clusters.eu.clusters) }
`,
				Check: resource.TestCheckOutput("count", "2"),
			},
		},
	})
}
