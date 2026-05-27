package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

// Attaching a public IP flags the VM restart_required; a cloudless_vm_restart
// wired after the attachment should issue exactly one restart and report it.
func TestUnitVMRestart_RestartsAfterAttach(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
  boot_disk { storage_id = cloudless_storage.boot.id }
}

resource "cloudless_public_ip" "edge" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "edge"
  address_type = "V4"
}

resource "cloudless_vm_public_ip_attachment" "att" {
  vm_id        = cloudless_vm.app.id
  public_ip_id = cloudless_public_ip.edge.id
}

resource "cloudless_vm_restart" "main" {
  vm_id = cloudless_vm.app.id
  triggers = {
    public_ip = cloudless_vm_public_ip_attachment.att.id
  }
  depends_on = [cloudless_vm_public_ip_attachment.att]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm_restart.main", "restarted", "true"),
					resource.TestCheckResourceAttrPair(
						"cloudless_vm_restart.main", "vm_id",
						"cloudless_vm.app", "id",
					),
				),
			},
		},
	})
}
