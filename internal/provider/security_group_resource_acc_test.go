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
		_, err := c.GetSecurityGroup(ctx, id)
		return err
	})
}

func TestAccSecurityGroup_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	name := "tf-acc-sg-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             securityGroupDestroy(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" {
  region = "DE"
}

resource "cloudless_security_group" "web" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = %q
  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_security_group.web", "name", name),
					resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress_mode", "allow_listed"),
					resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress.#", "1"),
				),
			},
			{
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" { region = "DE" }

resource "cloudless_security_group" "web" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = %q
  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
}
`, name+"-renamed"),
				Check: resource.TestCheckResourceAttr("cloudless_security_group.web", "name", name+"-renamed"),
			},
			{
				ResourceName:      "cloudless_security_group.web",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
