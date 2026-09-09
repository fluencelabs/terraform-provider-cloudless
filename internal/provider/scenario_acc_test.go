package provider_test

// Stage counterparts of the scenarios the mock cannot prove: real restart
// behaviour and the API's own rules about who may hold a public IP.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tfacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/provider/acctest"
)

const accCatalog = `
data "cloudless_clusters" "all" {}

locals {
  cluster_id = data.cloudless_clusters.all.clusters[0].id
}

data "cloudless_vm_configurations" "all" {}

data "cloudless_default_images" "all" {}
`

// accVM renders a VM booting from the Ubuntu catalog image with the given
// network blocks.
func accVM(name, suffix, nics string) string {
	return fmt.Sprintf(`
resource "cloudless_vm" %[1]q {
  cluster_id       = local.cluster_id
  name             = "tf-acc-%[1]s-%[2]s"
  configuration_id = data.cloudless_vm_configurations.all.configurations[0].id

  boot_disk {
    volume_gb = 40
    image_id  = [for i in data.cloudless_default_images.all.images : i.id if i.slug == "ubuntu-24-04-x64"][0]
  }
%[3]s
}
`, name, suffix, nics)
}

// A web server on stage: 443 open, own public IP. Opening 22 later edits the
// security group only; the VM must not be touched or restarted.
func TestAccScenario_WebServerOpenPortLater(t *testing.T) {
	factories := acctest.Setup(t)
	vpcID, subnetID := acctest.DefaultNetwork(t)
	suffix := tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	var vmID string

	sg := func(ports ...string) string {
		var rules strings.Builder
		for _, p := range ports {
			fmt.Fprintf(&rules, `
  ingress {
    protocol = "tcp"
    ports    = %q
    cidr     = "0.0.0.0/0"
  }`, p)
		}
		return fmt.Sprintf(`
resource "cloudless_security_group" "web" {
  vpc_id       = %q
  name         = "tf-acc-web-%s"
  ingress_mode = "allow_listed"%s
}
`, vpcID, suffix, rules.String())
	}
	nics := fmt.Sprintf(`
  network_interface {
    type              = "private"
    subnet_id         = %q
    security_group_id = cloudless_security_group.web.id
  }
  network_interface {
    type = "public"
  }
`, subnetID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             vmDestroy(),
		Steps: []resource.TestStep{
			{
				Config: accCatalog + sg("443") + accVM("web", suffix, nics),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.web", "status", "launched"),
					resource.TestCheckResourceAttr("cloudless_vm.web", "restart_required", "false"),
					captureID("cloudless_vm.web", &vmID),
				),
			},
			{
				Config: accCatalog + sg("443", "22") + accVM("web", suffix, nics),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cloudless_security_group.web", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("cloudless_vm.web", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					requireSameID("cloudless_vm.web", &vmID),
					resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress.#", "2"),
					resource.TestCheckResourceAttr("cloudless_vm.web", "restart_required", "false"),
				),
			},
		},
	})
}

// A reserved IP moves from one live VM to another in a single apply. This is
// the case the mock cannot prove: the API refuses to attach an IP that is
// still held, so ordering between the two updates matters
// (graph @cloudless/fluence, node #1824).
func TestAccScenario_MoveReservedIPBetweenVMs(t *testing.T) {
	factories := acctest.Setup(t)
	_, subnetID := acctest.DefaultNetwork(t)
	suffix := tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	var oldID, newID string

	ip := fmt.Sprintf(`
resource "cloudless_public_ip" "edge" {
  cluster_id   = local.cluster_id
  name         = "tf-acc-edge-%s"
  address_type = "V4"
}
`, suffix)
	private := fmt.Sprintf(`
  network_interface {
    type      = "private"
    subnet_id = %q
  }
`, subnetID)
	withIP := private + `
  network_interface {
    type         = "public"
    public_ip_id = cloudless_public_ip.edge.id
  }
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             vmDestroy(),
		Steps: []resource.TestStep{
			{
				Config: accCatalog + ip + accVM("old", suffix, withIP) + accVM("new", suffix, private),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"cloudless_vm.old",
						"public_ip_id",
						"cloudless_public_ip.edge",
						"id",
					),
					captureID("cloudless_vm.old", &oldID),
					captureID("cloudless_vm.new", &newID),
				),
			},
			{
				Config: accCatalog + ip + accVM("old", suffix, private) + accVM("new", suffix, withIP),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("cloudless_vm.old", "public_ip_id"),
					resource.TestCheckResourceAttrPair(
						"cloudless_vm.new",
						"public_ip_id",
						"cloudless_public_ip.edge",
						"id",
					),
					resource.TestCheckResourceAttr("cloudless_vm.new", "restart_required", "false"),
					func(s *terraform.State) error {
						if s.RootModule().Resources["cloudless_vm.old"].Primary.ID != oldID ||
							s.RootModule().Resources["cloudless_vm.new"].Primary.ID != newID {
							return errors.New("moving the IP must not recreate either VM")
						}
						return nil
					},
				),
			},
		},
	})
}
