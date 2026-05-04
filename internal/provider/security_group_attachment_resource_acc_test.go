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

func sgAttachmentDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "cloudless_security_group_attachment" {
				continue
			}
			ifaces, err := c.ListVMInterfaces(context.Background(), rs.Primary.Attributes["vm_id"])
			if err != nil {
				if client.IsNotFound(err) {
					continue
				}
				return err
			}
			for _, ni := range ifaces {
				if ni.ID == rs.Primary.Attributes["network_interface_id"] && ni.SecurityGroupID != nil {
					return fmt.Errorf("interface %s still bound to SG %s", ni.ID, *ni.SecurityGroupID)
				}
			}
		}
		return nil
	}
}

func TestAccSecurityGroupAttachment_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	bootName := "tf-acc-boot-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	vmName := "tf-acc-vm-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	sgName := "tf-acc-sg-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             sgAttachmentDestroy(),
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

resource "cloudless_security_group" "web" {
  cluster_id = data.cloudless_cluster.main.id
  name       = %q
}

resource "cloudless_security_group_attachment" "att" {
  network_interface_id = cloudless_vm.app.network_interface_ids[0]
  security_group_id    = cloudless_security_group.web.id
}
`, bootName, vmName, sgName),
				Check: resource.TestCheckResourceAttrPair(
					"cloudless_security_group_attachment.att", "vm_id",
					"cloudless_vm.app", "id",
				),
			},
			{
				ResourceName:      "cloudless_security_group_attachment.att",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["cloudless_security_group_attachment.att"]
					return rs.Primary.Attributes["vm_id"] + ":" + rs.Primary.Attributes["network_interface_id"], nil
				},
			},
		},
	})
}
