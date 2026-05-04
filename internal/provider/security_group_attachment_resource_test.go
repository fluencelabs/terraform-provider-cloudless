package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitSecurityGroupAttachment_BindAndUnbind(t *testing.T) {
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

resource "cloudless_security_group" "web" {
  cluster_id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name       = "web"
}

resource "cloudless_security_group_attachment" "att" {
  network_interface_id = cloudless_vm.app.network_interface_ids[0]
  security_group_id    = cloudless_security_group.web.id
}
`,
				Check: resource.TestCheckResourceAttrPair(
					"cloudless_security_group_attachment.att", "vm_id",
					"cloudless_vm.app", "id",
				),
			},
		},
	})
}
