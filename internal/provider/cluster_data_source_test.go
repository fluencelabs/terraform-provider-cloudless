package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitClusterDataSource_FilterByRegion(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	h.Mock.SeedDatacenter("dc-de", "DE", "FRA", "fra-1")
	h.Mock.SeedDatacenter("dc-pl", "PL", "WAW", "waw-1")
	h.Mock.SeedCluster("cluster-de", "Cloudless-DE", "dc-de")
	h.Mock.SeedCluster("cluster-pl", "Cloudless-PL", "dc-pl")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_cluster" "de" {
  region = "DE"
}
output "id"   { value = data.cloudless_cluster.de.id }
output "city" { value = data.cloudless_cluster.de.city_code }
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckOutput("id", "cluster-de"),
					resource.TestCheckOutput("city", "FRA"),
				),
			},
		},
	})
}

func TestUnitClusterDataSource_AmbiguousErrors(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	h.Mock.SeedDatacenter("dc-de1", "DE", "FRA", "fra-1")
	h.Mock.SeedDatacenter("dc-de2", "DE", "BER", "ber-1")
	h.Mock.SeedCluster("cluster-fra", "Cloudless-FRA", "dc-de1")
	h.Mock.SeedCluster("cluster-ber", "Cloudless-BER", "dc-de2")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_cluster" "ambig" { region = "DE" }
`,
				ExpectError: regexpAmbiguous(),
			},
		},
	})
}

// regexpAmbiguous returns a compiled regex matching the diagnostic title used
// by cluster_data_source. Kept here so the message can evolve in one place.
func regexpAmbiguous() *regexp.Regexp { return regexp.MustCompile(`Ambiguous`) }
