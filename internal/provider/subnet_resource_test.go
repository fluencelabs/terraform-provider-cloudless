package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitSubnet_DerivesClusterIDFromVPC(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	h.Mock.SeedVPC("99999999-9999-4999-8999-999999999999", "main", "cluster-X")
	// (Subnet endpoints are auto-wired by mock.New(); no extra seeding needed.)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_subnet" "s" {
  vpc_id    = "99999999-9999-4999-8999-999999999999"
  name      = "demo"
  ipv4_cidr = "10.0.0.0/24"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_subnet.s", "cluster_id", "cluster-X"),
				),
			},
		},
	})
}

// A subnet without a CIDR is refused in the plan: the API needs at least one
// (the public spec marks both nullable, so only this rule stops a 400).
func TestUnitSubnet_NeedsACIDR(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	h.Mock.SeedVPC("99999999-9999-4999-8999-999999999999", "main", "cluster-X")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_subnet" "s" {
  vpc_id = "99999999-9999-4999-8999-999999999999"
  name   = "demo"
}
`,
				ExpectError: regexp.MustCompile(`(?s)ipv4_cidr.*ipv6_cidr`),
			},
		},
	})
}

// An IPv6-only subnet with egress left on is refused in the plan: the API
// answers 422 ipv6_egress_unsupported.
func TestUnitSubnet_IPv6OnlyNeedsEgressOff(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	h.Mock.SeedVPC("99999999-9999-4999-8999-999999999999", "main", "cluster-X")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_subnet" "s" {
  vpc_id    = "99999999-9999-4999-8999-999999999999"
  name      = "demo"
  ipv6_cidr = "2001:db8::/64"
}
`,
				ExpectError: regexp.MustCompile(`egress`),
			},
		},
	})
}

// With egress off, the same IPv6-only subnet is created.
func TestUnitSubnet_IPv6OnlyWithoutEgress(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	h.Mock.SeedVPC("99999999-9999-4999-8999-999999999999", "main", "cluster-X")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_subnet" "s" {
  vpc_id    = "99999999-9999-4999-8999-999999999999"
  name      = "demo"
  ipv6_cidr = "2001:db8::/64"
  egress    = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_subnet.s", "egress", "false"),
					resource.TestCheckResourceAttr("cloudless_subnet.s", "is_default", "false"),
				),
			},
		},
	})
}

// The default flag moves onto the subnet that asks for it, and away from the
// one that held it.
func TestUnitSubnet_TakesTheDefaultFlag(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	h.Mock.SeedVPC("99999999-9999-4999-8999-999999999999", "main", "cluster-X")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_subnet" "s" {
  vpc_id     = "99999999-9999-4999-8999-999999999999"
  name       = "demo"
  ipv4_cidr  = "10.0.1.0/24"
  is_default = true
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_subnet.s", "is_default", "true"),
			},
		},
	})
}

// The flag only moves; it cannot be put out.
func TestUnitSubnet_DefaultCannotBeCleared(t *testing.T) {
	h := tfharness.New(t)
	defer h.Close()
	h.Mock.SeedVPC("99999999-9999-4999-8999-999999999999", "main", "cluster-X")

	cfg := func(dflt string) string {
		return `
resource "cloudless_subnet" "s" {
  vpc_id     = "99999999-9999-4999-8999-999999999999"
  name       = "demo"
  ipv4_cidr  = "10.0.2.0/24"
  is_default = ` + dflt + `
}
`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{Config: cfg("true")},
			{Config: cfg("false"), ExpectError: regexp.MustCompile(`is_default cannot be turned off`)},
		},
	})
}
