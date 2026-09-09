package provider_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	tfacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/acctest"
)

// A VM assembled through the /v3 draft flow: boot disk from the image catalog,
// a private default interface with a security group, and a public IP the VM
// creates for itself — all in one apply. A second step rebinds the security
// group on the live VM. On destroy the VM-owned IP must be gone.
func TestAccVMNetwork_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	suffix := tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	vpcID, subnetID := acctest.DefaultNetwork(t)
	var ownedIP string

	base := fmt.Sprintf(`
data "cloudless_clusters" "all" {}

locals {
  # The acceptance account may live in any region; take the first cluster it can see.
  cluster_id = data.cloudless_clusters.all.clusters[0].id
}

data "cloudless_vm_configurations" "all" {}

data "cloudless_default_images" "all" {}

resource "cloudless_security_group" "web" {
  vpc_id = %[2]q
  name   = "tf-acc-sg-%[1]s"
}

resource "cloudless_security_group" "web2" {
  vpc_id = %[2]q
  name   = "tf-acc-sg2-%[1]s"
}
`, suffix, vpcID)

	vm := func(sg string) string {
		return base + fmt.Sprintf(`
resource "cloudless_vm" "app" {
  cluster_id       = local.cluster_id
  name             = "tf-acc-vm-%[1]s"
  configuration_id = data.cloudless_vm_configurations.all.configurations[0].id

  boot_disk {
    volume_gb = 40
    image_id  = [for i in data.cloudless_default_images.all.images : i.id if i.slug == "ubuntu-24-04-x64"][0]
  }

  network_interface {
    type              = "private"
    subnet_id         = %[3]q
    security_group_id = %[2]s
  }

  network_interface {
    type = "public"
  }
}
`, suffix, sg, subnetID)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(s *terraform.State) error {
			if err := vmDestroy()(s); err != nil {
				return err
			}
			if ownedIP == "" {
				return errors.New("VM-owned public IP id was never captured")
			}
			ip, err := acctest.RealClient().GetPublicIP(context.Background(), ownedIP)
			if err != nil {
				if client.IsNotFound(err) {
					return nil
				}
				return err
			}
			if gerr := acctest.GoneIf(ip.Status, nil); gerr == nil {
				return fmt.Errorf("VM-owned public IP %s still exists after destroy (status %s)", ownedIP, ip.Status)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: vm("cloudless_security_group.web.id"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "status", "launched"),
					resource.TestCheckResourceAttr("cloudless_vm.app", "restart_required", "false"),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "2"),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.0.default", "true"),
					resource.TestCheckResourceAttrPair(
						"cloudless_vm.app",
						"network_interface.0.security_group_id",
						"cloudless_security_group.web",
						"id",
					),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.1.address_type", "V4"),
					resource.TestCheckResourceAttrSet("cloudless_vm.app", "network_interface.1.public_ip_id"),
					captureAttr("cloudless_vm.app", "network_interface.1.public_ip_id", &ownedIP),
				),
			},
			{
				// Rebinding the security group is the one in-place network change
				// a live VM accepts; the provider restarts it afterwards.
				Config: vm("cloudless_security_group.web2.id"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "2"),
					resource.TestCheckResourceAttrPair(
						"cloudless_vm.app",
						"network_interface.0.security_group_id",
						"cloudless_security_group.web2",
						"id",
					),
					resource.TestCheckResourceAttr("cloudless_vm.app", "restart_required", "false"),
				),
			},
		},
	})
}
