package provider_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

const (
	// mockDefaultSubnet is the subnet the mock binds a fresh draft's default
	// interface to (mock.defaultDraftSubnetID).
	mockDefaultSubnet = "00000000-0000-4000-8000-0000000005b1"
	nicTestCluster    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	nicTestConfig     = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
	nicTestSubnet     = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	nicTestSubnet2    = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	nicTestBoot       = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// vmWithNICs renders a cloudless_vm with the given network_interface blocks.
func vmWithNICs(blocks string) string {
	const name = "app"
	return fmt.Sprintf(`
resource "cloudless_vm" %q {
  cluster_id       = %q
  name             = %q
  configuration_id = %q
  boot_disk { storage_id = %q }
%s
}
`, name, nicTestCluster, name, nicTestConfig, nicTestBoot, blocks)
}

// A VM with a private default interface bound to a security group and a
// public interface the VM creates for itself comes up in one apply with no
// restart pending.
func TestUnitVMNetwork_PrivateAndOwnedPublicInOneApply(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{{
			Config: `
resource "cloudless_security_group" "web" {
  vpc_id = "11111111-1111-4111-8111-111111111111"
  name   = "web"
}
` + vmWithNICs(`
  network_interface {
    type              = "private"
    subnet_id         = "`+nicTestSubnet+`"
    security_group_id = cloudless_security_group.web.id
    static_ips        = ["10.0.0.5"]
  }
  network_interface {
    type = "public"
  }
`),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("cloudless_vm.app", "status", "launched"),
				resource.TestCheckResourceAttr("cloudless_vm.app", "restart_required", "false"),
				resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "2"),
				resource.TestCheckResourceAttr(
					"cloudless_vm.app",
					"network_interface.0.default",
					"true",
				),
				resource.TestCheckResourceAttr(
					"cloudless_vm.app",
					"network_interface.0.subnet_id",
					nicTestSubnet,
				),
				resource.TestCheckResourceAttrPair(
					"cloudless_vm.app",
					"network_interface.0.security_group_id",
					"cloudless_security_group.web",
					"id",
				),
				resource.TestCheckResourceAttr(
					"cloudless_vm.app",
					"network_interface.0.static_ips.0",
					"10.0.0.5",
				),
				resource.TestCheckResourceAttrSet(
					"cloudless_vm.app",
					"network_interface.1.public_ip_id",
				),
				resource.TestCheckResourceAttr(
					"cloudless_vm.app",
					"network_interface.1.address_type",
					"V4",
				),
				resource.TestCheckResourceAttrPair(
					"cloudless_vm.app",
					"public_ip_id",
					"cloudless_vm.app",
					"network_interface.1.public_ip_id",
				),
				resource.TestCheckResourceAttr("cloudless_vm.app", "subnet_ids.0", nicTestSubnet),
			),
		}},
	})
	if got := h.Mock.RestartCount(); got != 0 {
		t.Fatalf(
			"assembling the network on the draft must not restart the VM, got %d restarts",
			got,
		)
	}
}

// Adding a public interface with an existing IP to a live VM is an in-place
// update that the provider follows with the restart the API asks for.
func TestUnitVMNetwork_AddPublicToLiveVMRestartsInPlace(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	var vmID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet + `"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "1"),
					captureID("cloudless_vm.app", &vmID),
				),
			},
			{
				Config: `
resource "cloudless_public_ip" "edge" {
  cluster_id   = "` + nicTestCluster + `"
  name         = "edge"
  address_type = "V4"
}
` + vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "`+nicTestSubnet+`"
  }
  network_interface {
    type         = "public"
    public_ip_id = cloudless_public_ip.edge.id
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"cloudless_vm.app",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					requireSameID("cloudless_vm.app", &vmID),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "2"),
					resource.TestCheckResourceAttrPair(
						"cloudless_vm.app",
						"network_interface.1.public_ip_id",
						"cloudless_public_ip.edge",
						"id",
					),
					resource.TestCheckResourceAttr("cloudless_vm.app", "restart_required", "false"),
				),
			},
			{
				Config: `
resource "cloudless_public_ip" "edge" {
  cluster_id   = "` + nicTestCluster + `"
  name         = "edge"
  address_type = "V4"
}
` + vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "`+nicTestSubnet+`"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					requireSameID("cloudless_vm.app", &vmID),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "1"),
				),
			},
		},
	})
	// Observed on stage: attaching a reserved IP applies immediately, only
	// removing an interface flags restart_required.
	if got := h.Mock.RestartCount(); got != 1 {
		t.Fatalf(
			"detaching on a live VM should restart it once, attaching not at all; got %d restarts",
			got,
		)
	}
}

// Changing the default interface's subnet cannot happen on a live VM and is
// planned as a replacement.
func TestUnitVMNetwork_DefaultSubnetChangesInPlace(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	var vmID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{Config: vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet + `"
  }
`),
				Check: captureID("cloudless_vm.app", &vmID)},
			{
				// The default interface is repointed and the VM restarted;
				// only a subnet in another cluster would need a new VM.
				Config: vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet2 + `"
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"cloudless_vm.app",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					requireSameID("cloudless_vm.app", &vmID),
					resource.TestCheckResourceAttr(
						"cloudless_vm.app",
						"network_interface.0.subnet_id",
						nicTestSubnet2,
					),
					resource.TestCheckResourceAttr(
						"cloudless_vm.app",
						"subnet_ids.0",
						nicTestSubnet2,
					),
				),
			},
		},
	})
}

func TestUnitVMNetwork_ReorderedInterfacesPlanEmpty(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	cfg := `
resource "cloudless_public_ip" "edge" {
  cluster_id   = "` + nicTestCluster + `"
  name         = "edge"
  address_type = "V4"
}
` + vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "`+nicTestSubnet+`"
  }
  network_interface {
    type         = "public"
    public_ip_id = cloudless_public_ip.edge.id
  }
  network_interface {
    type      = "private"
    subnet_id = "`+nicTestSubnet2+`"
  }
`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: h.Mock.ReverseInterfaces,
				Config:    cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// Invalid interface layouts fail at plan time, before any API call.
func TestUnitVMNetwork_InvalidLayoutsFailAtPlan(t *testing.T) {
	cases := []struct {
		name, blocks, want string
	}{
		{"public only", `
  network_interface {
    type = "public"
  }
`, "at least one private interface"},
		{"two defaults", `
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet + `"
    default   = true
  }
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet2 + `"
    default   = true
  }
`, "both default"},
		{"public default", `
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet + `"
  }
  network_interface {
    type    = "public"
    default = true
  }
`, "only a private interface can be the default"},
		{"subnet on public", `
  network_interface {
    type      = "public"
    subnet_id = "` + nicTestSubnet + `"
  }
`, "subnet_id applies to private interfaces only"},
		{"address_type with public_ip_id", `
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet + `"
  }
  network_interface {
    type         = "public"
    public_ip_id = "` + nicTestBoot + `"
    address_type = "V4"
  }
`, "address_type applies only to a VM-created IP"},
		{"two private blocks without subnet", `
  network_interface {
    type = "private"
  }
  network_interface {
    type = "private"
  }
`, "both omit subnet_id"},
		{"unbound private beside another default", `
  network_interface {
    type = "private"
  }
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet + `"
    default   = true
  }
`, "omits subnet_id but network_interface"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tfharness.New(t)
			defer h.Close()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: h.Factories,
				Steps: []resource.TestStep{{
					Config:      vmWithNICs(tc.blocks),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(tc.want),
				}},
			})
		})
	}
}

// Destroying a VM releases the public IP it created for itself.
func TestUnitVMNetwork_DestroyReleasesOwnedPublicIP(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	var ipID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		CheckDestroy: func(*terraform.State) error {
			if ipID == "" {
				return errors.New("owned public IP id was never captured")
			}
			if _, err := h.Client.GetPublicIP(context.Background(), ipID); err == nil {
				return fmt.Errorf("VM-owned public IP %s still exists after destroy", ipID)
			} else if !client.IsNotFound(err) {
				return err
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet + `"
  }
  network_interface {
    type = "public"
  }
`),
			Check: captureAttr("cloudless_vm.app", "network_interface.1.public_ip_id", &ipID),
		}},
	})
}

// A VM created without network_interface blocks whose configuration later
// declares them: on the server-default subnet the blocks are adopted in
// place; on another subnet the VM is replaced, because a live default
// interface cannot be repointed.
func TestUnitVMNetwork_AdoptBlocksAfterCreate(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	var vmID string
	base := vmWithNICs("")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: base,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "0"),
					captureID("cloudless_vm.app", &vmID),
				),
			},
			{
				// Same subnet as the server default: adopted in place.
				Config: vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "` + mockDefaultSubnet + `"
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"cloudless_vm.app",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					requireSameID("cloudless_vm.app", &vmID),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "1"),
				),
			},
			{
				// A different default subnet in the same cluster: repointed
				// on the same VM.
				Config: vmWithNICs(`
  network_interface {
    type      = "private"
    subnet_id = "` + nicTestSubnet2 + `"
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"cloudless_vm.app",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					requireSameID("cloudless_vm.app", &vmID),
					resource.TestCheckResourceAttr(
						"cloudless_vm.app",
						"network_interface.0.subnet_id",
						nicTestSubnet2,
					),
				),
			},
		},
	})
}

// The default interface may leave subnet_id out: the VM keeps the cluster's
// default subnet and the id it landed on is computed into state.
func TestUnitVMNetwork_DefaultSubnetMayBeOmitted(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: vmWithNICs(`
  network_interface {
    type = "private"
  }

  network_interface {
    type = "public"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface.#", "2"),
					resource.TestCheckResourceAttr(
						"cloudless_vm.app",
						"network_interface.0.default",
						"true",
					),
					resource.TestCheckResourceAttr(
						"cloudless_vm.app", "network_interface.0.subnet_id", mockDefaultSubnet),
				),
			},
			{
				// The computed subnet does not come back as a diff.
				Config: vmWithNICs(
					"\n  network_interface {\n    type = \"private\"\n  }\n\n  network_interface {\n    type = \"public\"\n  }\n",
				),
				PlanOnly: true,
			},
		},
	})
}
