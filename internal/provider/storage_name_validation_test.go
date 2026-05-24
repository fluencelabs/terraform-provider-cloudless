package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

// The Fluence API caps resource names at 25 chars (and restricts the charset).
// The storage resource wires the ResourceName validator so an over-long name
// is rejected at plan time rather than failing at apply — this is the exact
// case that bit `tf-cloudless-infra-vm-boot` (26 chars).
func TestUnitStorage_RejectsTooLongName(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "11111111-1111-1111-1111-111111111111"
  name         = "tf-cloudless-infra-vm-boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = true
}
`,
				ExpectError: regexp.MustCompile(`Invalid name`),
			},
		},
	})
}
