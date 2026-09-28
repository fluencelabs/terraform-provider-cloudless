package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

const estimateBase = `
data "cloudless_vm_estimate" "app" {
  name             = "app"
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"

  boot_disk {
    volume_gb = 40
    image_id  = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
  }

  network_interface {
    type      = "private"
    subnet_id = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
  }
%s
}
`

// A price shows up in the plan, before anything is created.
func TestUnitVMEstimate_PricesASpec(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(estimateBase, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					// 0.05 for the VM plus 40 GB of boot disk at 0.0001.
					resource.TestCheckResourceAttr("data.cloudless_vm_estimate.app", "hourly_total", "0.0540"),
					resource.TestCheckResourceAttr("data.cloudless_vm_estimate.app", "hourly_incremental", "0.0540"),
					resource.TestCheckResourceAttr("data.cloudless_vm_estimate.app", "currency", "USD"),
					resource.TestCheckResourceAttrSet("data.cloudless_vm_estimate.app", "calculated_at"),
				),
			},
		},
	})
}

// A public address and a replicated data disk both cost, and both land in the
// figure the plan shows.
func TestUnitVMEstimate_CountsAddressAndDisks(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(estimateBase, `
  network_interface {
    type = "public"
  }

  data_disk {
    volume_gb  = 100
    replicated = true
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					// 0.05 VM + 0.004 address + 40 GB boot + 100 GB replicated.
					resource.TestCheckResourceAttr("data.cloudless_vm_estimate.app", "hourly_total", "0.0780"),
				),
			},
		},
	})
}

// An existing disk is already billed, so it counts in the total and not in
// what this specification would newly cost.
func TestUnitVMEstimate_ExistingDiskIsNotIncremental(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(estimateBase, `
  data_disk {
    storage_id = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudless_vm_estimate.app", "hourly_total", "0.0565"),
					resource.TestCheckResourceAttr("data.cloudless_vm_estimate.app", "hourly_incremental", "0.0540"),
				),
			},
		},
	})
}

func TestUnitVMEstimate_BootDiskMustBeComplete(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_vm_estimate" "app" {
  name             = "app"
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"

  boot_disk {
    volume_gb = 40
  }

  network_interface {
    type      = "private"
    subnet_id = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
  }
}
`,
				ExpectError: regexp.MustCompile(`boot_disk needs either storage_id`),
			},
		},
	})
}
