package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestUnitSecurityGroup_AllowAll(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_security_group" "wide" {
  cluster_id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name       = "wide"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_security_group.wide", "ingress_mode", "allow_all"),
					resource.TestCheckResourceAttr("cloudless_security_group.wide", "egress_mode", "allow_all"),
					resource.TestCheckResourceAttr("cloudless_security_group.wide", "status", "ready"),
				),
			},
		},
	})
}

func TestUnitSecurityGroup_AllowListed(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_security_group" "web" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "web"
  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress.#", "1"),
			},
		},
	})
}

func TestUnitSecurityGroup_DenyAll(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_security_group" "tight" {
  cluster_id  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name        = "tight"
  egress_mode = "deny_all"
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_security_group.tight", "egress_mode", "deny_all"),
			},
		},
	})
}

func TestUnitSecurityGroup_AllowListedRequiresBlocks(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_security_group" "broken" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "broken"
  ingress_mode = "allow_listed"
  # No ingress blocks → should fail validation
}
`,
				ExpectError: regexpAtLeastOneRule(),
			},
		},
	})
}

func regexpAtLeastOneRule() *regexp.Regexp {
	return regexp.MustCompile(`at least one ingress block`)
}
