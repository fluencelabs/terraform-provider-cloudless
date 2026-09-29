package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/cloudless/terraform-provider-cloudless/internal/provider/acctest"
)

// The price comes back from the live API as a decimal string with far more
// precision than a float would survive (observed on stage: seventeen places),
// which is why it is carried as a string end to end.
func TestAccVMEstimate_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	_, subnetID := acctest.DefaultNetwork(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "cloudless_clusters" "all" {}

data "cloudless_vm_configurations" "all" {}

data "cloudless_default_images" "all" {}

data "cloudless_vm_estimate" "app" {
  name             = "tf-acc-estimate"
  cluster_id       = data.cloudless_clusters.all.clusters[0].id
  configuration_id = data.cloudless_vm_configurations.all.configurations[0].id

  boot_disk {
    volume_gb = 40
    image_id  = [for i in data.cloudless_default_images.all.images : i.id if i.slug == "ubuntu-24-04-x64"][0]
  }

  network_interface {
    type      = "private"
    subnet_id = %q
  }

  network_interface {
    type = "public"
  }
}
`, subnetID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudless_vm_estimate.app", "currency", "USD"),
					resource.TestMatchResourceAttr(
						"data.cloudless_vm_estimate.app", "hourly_total", regexp.MustCompile(`^0\.\d+$`)),
					resource.TestMatchResourceAttr(
						"data.cloudless_vm_estimate.app", "hourly_incremental", regexp.MustCompile(`^0\.\d+$`)),
					resource.TestCheckResourceAttrSet("data.cloudless_vm_estimate.app", "calculated_at"),
				),
			},
		},
	})
}
