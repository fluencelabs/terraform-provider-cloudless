package provider_test

// Scenario tests: whole configurations the way a user writes them, evolved
// across apply steps. Each scenario states the user's intent in its name and
// asserts the outcome a user would check (ids kept, no surprise restarts,
// resources released), not provider internals.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

const (
	scCluster = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	scConfig  = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
	scVPC     = "11111111-1111-4111-8111-111111111111"
	scSubnet  = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	scImage   = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	scImage2  = "cccccccc-cccc-4ccc-8ccc-cccccccccc22"
)

// scVM renders a VM with a catalog boot disk and the given extra body.
func scVM(name, extra string) string {
	return fmt.Sprintf(`
resource "cloudless_vm" %[1]q {
  cluster_id       = %[2]q
  name             = %[1]q
  configuration_id = %[3]q
  boot_disk {
    volume_gb = 40
    image_id  = %[4]q
  }
%[5]s
}
`, name, scCluster, scConfig, scImage, extra)
}

func scWebSG(ports ...string) string {
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
  name         = "web"
  ingress_mode = "allow_listed"%s
}
`, scVPC, rules.String())
}

const scWebNICs = `
  network_interface {
    type              = "private"
    subnet_id         = "` + scSubnet + `"
    security_group_id = cloudless_security_group.web.id
  }
  network_interface {
    type = "public"
  }
`

// A web server: reachable on 443 with its own public IP. Later the operator
// opens 22 for maintenance by editing the security group — the VM itself is
// untouched and never restarted.
func TestScenario_WebServer_OpenPortLater(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	var vmID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: scWebSG("443") + scVM("web", scWebNICs),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.web", "status", "launched"),
					resource.TestCheckResourceAttrSet("cloudless_vm.web", "public_ip_id"),
					resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress.#", "1"),
					captureID("cloudless_vm.web", &vmID),
				),
			},
			{
				Config: scWebSG("443", "22") + scVM("web", scWebNICs),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cloudless_security_group.web", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("cloudless_vm.web", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					requireSameID("cloudless_vm.web", &vmID),
					resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress.#", "2"),
				),
			},
		},
	})
	if got := h.Mock.RestartCount(); got != 0 {
		t.Fatalf("editing security group rules must not restart the VM, got %d restarts", got)
	}
}

// A bastion with a public IP and an application VM that is private-only and
// accepts traffic solely from the bastion's security group.
func TestScenario_BastionAndPrivateApp(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	cfg := fmt.Sprintf(`
resource "cloudless_security_group" "bastion" {
  vpc_id       = %[1]q
  name         = "bastion"
  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "22"
    cidr     = "0.0.0.0/0"
  }
}

resource "cloudless_security_group" "app" {
  vpc_id       = %[1]q
  name         = "app"
  ingress_mode = "allow_listed"
  ingress {
    protocol          = "tcp"
    ports             = "8080"
    security_group_id = cloudless_security_group.bastion.id
  }
}
`, scVPC) + scVM("bastion", `
  network_interface {
    type              = "private"
    subnet_id         = "`+scSubnet+`"
    security_group_id = cloudless_security_group.bastion.id
  }
  network_interface {
    type = "public"
  }
`) + scVM("app", `
  network_interface {
    type              = "private"
    subnet_id         = "`+scSubnet+`"
    security_group_id = cloudless_security_group.app.id
  }
`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("cloudless_vm.bastion", "public_ip_id"),
				resource.TestCheckNoResourceAttr("cloudless_vm.app", "public_ip_id"),
				resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "1"),
				resource.TestCheckResourceAttrPair(
					"cloudless_vm.app",
					"network_interface.0.security_group_id",
					"cloudless_security_group.app",
					"id",
				),
				resource.TestCheckResourceAttrPair(
					"cloudless_security_group.app",
					"ingress.0.security_group_id",
					"cloudless_security_group.bastion",
					"id",
				),
			),
		}},
	})
}

// A reserved public IP is moved from an old VM to its replacement in one
// apply: both VMs update in place and the address ends up on the new one.
func TestScenario_MoveReservedIPBetweenVMs(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	var oldID, newID string

	ip := fmt.Sprintf(`
resource "cloudless_public_ip" "edge" {
  cluster_id   = %q
  name         = "edge"
  address_type = "V4"
}
`, scCluster)
	private := `
  network_interface {
    type      = "private"
    subnet_id = "` + scSubnet + `"
  }
`
	withIP := private + `
  network_interface {
    type         = "public"
    public_ip_id = cloudless_public_ip.edge.id
  }
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: ip + scVM("old", withIP) + scVM("new", private),
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
				Config: ip + scVM("old", private) + scVM("new", withIP),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cloudless_vm.old", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("cloudless_vm.new", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("cloudless_vm.old", "public_ip_id"),
					resource.TestCheckResourceAttrPair(
						"cloudless_vm.new",
						"public_ip_id",
						"cloudless_public_ip.edge",
						"id",
					),
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

// A database VM outgrows its disk: the data volume is resized in place and a
// second volume is attached, without recreating the VM.
func TestScenario_GrowStorageAndAddDisk(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	var vmID string

	disk := func(name string, gb int) string {
		return fmt.Sprintf(`
resource "cloudless_storage" %[1]q {
  cluster_id   = %[2]q
  name         = %[1]q
  storage_type = "NVME"
  volume_gb    = %[3]d
  replicated   = false
}
`, name, scCluster, gb)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: disk("data1", 100) + scVM("db", `
  data_disk_ids = [cloudless_storage.data1.id]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.db", "data_disk_ids.#", "1"),
					captureID("cloudless_vm.db", &vmID),
				),
			},
			{
				Config: disk("data1", 200) + disk("data2", 100) + scVM("db", `
  data_disk_ids = [cloudless_storage.data1.id, cloudless_storage.data2.id]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cloudless_storage.data1", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("cloudless_vm.db", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_storage.data1", "volume_gb", "200"),
					resource.TestCheckResourceAttr("cloudless_vm.db", "data_disk_ids.#", "2"),
					func(s *terraform.State) error {
						if s.RootModule().Resources["cloudless_vm.db"].Primary.ID != vmID {
							return errors.New("adding a disk must not recreate the VM")
						}
						return nil
					},
				),
			},
		},
	})
}

// Renaming a VM and its security group is an in-place update.
func TestScenario_RenameInPlace(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	var vmID string
	sg := func(name string) string {
		return fmt.Sprintf(`
resource "cloudless_security_group" "web" {
  vpc_id = %q
  name   = %q
}
`, scVPC, name)
	}
	vm := func(name string) string {
		return fmt.Sprintf(`
resource "cloudless_vm" "web" {
  cluster_id       = %q
  name             = %q
  configuration_id = %q
  boot_disk {
    volume_gb = 40
    image_id  = %q
  }
  network_interface {
    type              = "private"
    subnet_id         = %q
    security_group_id = cloudless_security_group.web.id
  }
}
`, scCluster, name, scConfig, scImage, scSubnet)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{Config: sg("web") + vm("web-1"), Check: captureID("cloudless_vm.web", &vmID)},
			{
				Config: sg("web-prod") + vm("web-prod-1"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cloudless_vm.web", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("cloudless_security_group.web", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.web", "name", "web-prod-1"),
					resource.TestCheckResourceAttr("cloudless_security_group.web", "name", "web-prod"),
					func(s *terraform.State) error {
						if s.RootModule().Resources["cloudless_vm.web"].Primary.ID != vmID {
							return errors.New("rename must not recreate the VM")
						}
						return nil
					},
				),
			},
		},
	})
}

// A VM created outside Terraform is imported by id: its interfaces show up as
// blocks and a matching configuration plans empty afterwards.
func TestScenario_ImportExistingVM(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	cfg := scVM("legacy", scWebNICs)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{Config: scWebSG("443") + cfg},
			{
				ResourceName:      "cloudless_vm.legacy",
				ImportState:       true,
				ImportStateVerify: true,
				// boot_disk is configuration-only (Read yields boot_disk_id);
				// address_type records IP ownership, which an import cannot know.
				ImportStateVerifyIgnore: []string{"boot_disk", "network_interface.1.address_type"},
			},
			{
				Config: scWebSG("443") + cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// Switching the boot image is a rebuild: the VM is replaced, and the public
// IP it owned is released with the old VM and recreated with the new one.
func TestScenario_ReplaceBootImageRebuildsVM(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	var vmID, oldIP string

	vm := func(image string) string {
		return scWebSG("443") + fmt.Sprintf(`
resource "cloudless_vm" "web" {
  cluster_id       = %q
  name             = "web"
  configuration_id = %q
  boot_disk {
    volume_gb = 40
    image_id  = %q
  }
%s
}
`, scCluster, scConfig, image, scWebNICs)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: vm(scImage),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID("cloudless_vm.web", &vmID),
					captureAttr("cloudless_vm.web", "public_ip_id", &oldIP),
				),
			},
			{
				Config: vm(scImage2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cloudless_vm.web", plancheck.ResourceActionReplace),
					},
				},
				Check: func(s *terraform.State) error {
					rs := s.RootModule().Resources["cloudless_vm.web"].Primary
					if rs.ID == vmID {
						return errors.New("changing the image must rebuild the VM")
					}
					if rs.Attributes["public_ip_id"] == oldIP {
						return errors.New("the rebuilt VM should own a fresh public IP")
					}
					if _, err := h.Client.GetPublicIP(context.Background(), oldIP); err == nil ||
						!client.IsNotFound(err) {
						return fmt.Errorf("old VM-owned IP %s should be released: %w", oldIP, err)
					}
					return nil
				},
			},
		},
	})
}

// The simplest VM: no network blocks at all. The server default interface is
// used, mirrored in subnet_ids, and the configuration stays stable.
func TestScenario_MinimalVMUsesServerDefaultNetwork(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	cfg := scVM("tiny", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.tiny", "network_interface.#", "0"),
					resource.TestCheckResourceAttr("cloudless_vm.tiny", "subnet_ids.#", "1"),
					resource.TestCheckResourceAttr("cloudless_vm.tiny", "network_interface_ids.#", "1"),
				),
			},
			{
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}
