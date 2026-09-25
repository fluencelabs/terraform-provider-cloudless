package provider_test

import (
	"context"
	"fmt"
	"testing"

	tfacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/provider/acctest"
)

func vmDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return acctest.CheckDestroy(c, "cloudless_vm", func(ctx context.Context, id string) error {
		got, err := c.GetVM(ctx, id)
		if err != nil {
			return err
		}
		return acctest.GoneIf(got.Status, nil)
	})
}

func TestAccVM_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	bootName := "tf-acc-boot-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	vmName := "tf-acc-vm-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             vmDestroy(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "cloudless_clusters" "all" {}

locals {
  # The acceptance account may live in any region; take the first cluster it can see.
  cluster_id = data.cloudless_clusters.all.clusters[0].id
}

data "cloudless_vm_configurations" "all" {}

data "cloudless_default_images" "all" {}

resource "cloudless_storage" "boot" {
  cluster_id   = local.cluster_id
  name         = %q
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  image_id     = [for i in data.cloudless_default_images.all.images : i.download_url if i.slug == "ubuntu-24-04-x64"][0]
}

resource "cloudless_vm" "app" {
  cluster_id       = local.cluster_id
  name             = %q
  configuration_id = data.cloudless_vm_configurations.all.configurations[0].id

  boot_disk {
    storage_id = cloudless_storage.boot.id
  }
}
`, bootName, vmName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "name", vmName),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface_ids.#", "1"),
				),
			},
			{
				ResourceName:      "cloudless_vm.app",
				ImportState:       true,
				ImportStateVerify: true,
				// boot_disk is a configuration-only block; Read populates
				// boot_disk_id (the computed string) instead. This is a
				// schema decision, not an API quirk — see vm_resource.fill.
				//
				// network_interface differs on purpose: this VM is declared
				// without blocks, so its state carries none, while an import
				// renders every interface the API reports (vmResource.
				// ImportState) — that is what makes an imported VM adoptable.
				ImportStateVerifyIgnore: []string{"boot_disk", "network_interface"},
			},
		},
	})
}
