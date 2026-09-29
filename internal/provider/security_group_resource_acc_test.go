package provider_test

import (
	"context"
	"fmt"
	"testing"

	tfacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/provider/acctest"
)

func securityGroupDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return acctest.CheckDestroy(c, "cloudless_security_group", func(ctx context.Context, id string) error {
		got, err := c.GetSecurityGroup(ctx, id)
		if err != nil {
			return err
		}
		return acctest.GoneIf(got.Status, nil)
	})
}

func TestAccSecurityGroup_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	vpcID, _ := acctest.DefaultNetwork(t)
	name := "tf-acc-sg-" + tfacctest.RandStringFromCharSet(
		6,
		tfacctest.CharSetAlphaNum,
	) // API: lowercase, digits, hyphens, max 25 chars

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             securityGroupDestroy(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`

resource "cloudless_security_group" "web" {
  vpc_id   = %[2]q
  name         = %[1]q
  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
}
`, name, vpcID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_security_group.web", "name", name),
					resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress_mode", "allow_listed"),
					resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress.#", "1"),
				),
			},
			{
				Config: fmt.Sprintf(`

resource "cloudless_security_group" "web" {
  vpc_id   = %[2]q
  name         = %[1]q
  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
}
`, name+"-v2", vpcID),
				Check: resource.TestCheckResourceAttr("cloudless_security_group.web", "name", name+"-v2"),
			},
			{
				ResourceName:      "cloudless_security_group.web",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
