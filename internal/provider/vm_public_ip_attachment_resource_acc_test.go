package provider_test

import (
	"context"
	"fmt"
	"testing"

	tfacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/acctest"
)

func vmPublicIPAttachmentDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "cloudless_vm_public_ip_attachment" {
				continue
			}
			vm, err := c.GetVM(context.Background(), rs.Primary.Attributes["vm_id"])
			if err != nil {
				if client.IsNotFound(err) {
					continue
				}
				return err
			}
			if vm.PublicIP != nil && *vm.PublicIP == rs.Primary.Attributes["public_ip_id"] {
				return fmt.Errorf("vm %s still has public IP %s attached", vm.ID, *vm.PublicIP)
			}
		}
		return nil
	}
}

func TestAccVMPublicIPAttachment_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	bootName := "tf-acc-boot-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	vmName := "tf-acc-vm-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	pipName := "tf-acc-pip-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             vmPublicIPAttachmentDestroy(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" {
  region = "DE"
}

data "cloudless_vm_configurations" "all" {}

data "cloudless_default_images" "all" {}

resource "cloudless_storage" "boot" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = %q
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = [for i in data.cloudless_default_images.all.images : i.download_url if i.slug == "ubuntu-24-04-x64"][0]
}

resource "cloudless_vm" "app" {
  cluster_id       = data.cloudless_cluster.main.id
  name             = %q
  configuration_id = data.cloudless_vm_configurations.all.configurations[0].id
  boot_disk { storage_id = cloudless_storage.boot.id }
}

resource "cloudless_public_ip" "edge" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = %q
  address_type = "V4"
}

resource "cloudless_vm_public_ip_attachment" "att" {
  vm_id        = cloudless_vm.app.id
  public_ip_id = cloudless_public_ip.edge.id
}
`, bootName, vmName, pipName),
				Check: resource.TestCheckResourceAttrPair(
					"cloudless_vm_public_ip_attachment.att", "vm_id",
					"cloudless_vm.app", "id",
				),
			},
			{
				ResourceName:      "cloudless_vm_public_ip_attachment.att",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["cloudless_vm_public_ip_attachment.att"]
					return rs.Primary.Attributes["vm_id"] + ":" + rs.Primary.Attributes["public_ip_id"], nil
				},
			},
		},
	})
}
