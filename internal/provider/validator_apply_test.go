package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

// TestAccValidator_RejectsInvalidUUID confirms the UUID validator wired into
// vpc.cluster_id actually fires at plan time, not just in unit-test isolation.
// Plan-time rejection means the API never sees the bad value.
func TestUnitValidator_RejectsInvalidUUID(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_vpc" "broken" {
  cluster_id = "not-a-uuid"
  name       = "broken"
}
`,
				ExpectError: regexp.MustCompile(`Invalid UUID`),
			},
		},
	})
}
