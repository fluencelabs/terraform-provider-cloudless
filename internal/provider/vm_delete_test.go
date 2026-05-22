package provider_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

// captureAttr stores resource attribute attr into *out for a later check.
func captureAttr(addr, attr string, out *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("resource %s not found in state", addr)
		}
		*out = rs.Primary.Attributes[attr]
		return nil
	}
}

// TestUnitVM_Delete_RemovesInlineBootDisk guards against the inline boot disk
// leaking on destroy. The inline boot disk is created by the VM (it is not a
// separate cloudless_storage resource), and VM terminate does not cascade it,
// so vmResource.Delete must delete the boot disk storage explicitly. Without
// that cleanup the storage volume is orphaned and keeps billing.
func TestUnitVM_Delete_RemovesInlineBootDisk(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	var bootDiskID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		CheckDestroy: func(*terraform.State) error {
			if bootDiskID == "" {
				return fmt.Errorf("boot_disk_id was never captured")
			}
			if _, err := h.Client.GetStorage(context.Background(), bootDiskID); err == nil {
				return fmt.Errorf("inline boot disk %s still exists after destroy (leaked)", bootDiskID)
			} else if !client.IsNotFound(err) {
				return fmt.Errorf("checking boot disk %s: %w", bootDiskID, err)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: `
resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

  boot_disk {
    name         = "boot"
    storage_type = "NVME"
    volume_gb    = 40
    replicated   = false
    os_image     = "https://example.com/img.qcow2"
  }
}
`,
			Check: captureAttr("cloudless_vm.app", "boot_disk_id", &bootDiskID),
		}},
	})
}
