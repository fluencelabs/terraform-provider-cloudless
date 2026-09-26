package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

// The singular catalog data sources are what keeps a VM readable: a slug
// instead of a list index or a comprehension over every image.
func TestUnitCatalog_SingularLookups(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_default_image" "ubuntu" {
  slug = "ubuntu-24-04-x64"
}

data "cloudless_vm_configuration" "small" {
  vcpu = 2
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.cloudless_default_image.ubuntu", "id"),
					resource.TestCheckResourceAttrSet("data.cloudless_default_image.ubuntu", "username"),
					resource.TestCheckResourceAttr("data.cloudless_vm_configuration.small", "vcpu", "2"),
					resource.TestCheckResourceAttrSet("data.cloudless_vm_configuration.small", "id"),
				),
			},
		},
	})
}

func TestUnitCatalog_SingularLookupFailures(t *testing.T) {
	cases := []struct{ name, cfg, want string }{
		{"no match", `
data "cloudless_default_image" "x" {
  slug = "plan9-4th-edition"
}
`, "No matching image"},
		{"ambiguous", `
data "cloudless_vm_configuration" "x" {}
`, "Ambiguous VM configuration filter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tfharness.New(t)
			defer h.Close()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: h.Factories,
				Steps: []resource.TestStep{
					{Config: tc.cfg, ExpectError: regexp.MustCompile(tc.want)},
				},
			})
		})
	}
}
