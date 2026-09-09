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

func publicIPDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return acctest.CheckDestroy(c, "cloudless_public_ip", func(ctx context.Context, id string) error {
		got, err := c.GetPublicIP(ctx, id)
		if err != nil {
			return err
		}
		return acctest.GoneIf(got.Status, nil)
	})
}

func TestAccPublicIP_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	name := "tf-acc-pip-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             publicIPDestroy(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "cloudless_clusters" "all" {}

locals {
  # The acceptance account may live in any region; take the first cluster it can see.
  cluster_id = data.cloudless_clusters.all.clusters[0].id
}

resource "cloudless_public_ip" "edge" {
  cluster_id   = local.cluster_id
  name         = %q
  address_type = "V4"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_public_ip.edge", "name", name),
					resource.TestCheckResourceAttr("cloudless_public_ip.edge", "address_type", "V4"),
					resource.TestCheckResourceAttrSet("cloudless_public_ip.edge", "address"),
				),
			},
			{
				Config: fmt.Sprintf(`
data "cloudless_clusters" "all" {}

locals {
  # The acceptance account may live in any region; take the first cluster it can see.
  cluster_id = data.cloudless_clusters.all.clusters[0].id
}

resource "cloudless_public_ip" "edge" {
  cluster_id   = local.cluster_id
  name         = %q
  address_type = "V4"
}
`, name+"-renamed"),
				Check: resource.TestCheckResourceAttr("cloudless_public_ip.edge", "name", name+"-renamed"),
			},
			{
				ResourceName:      "cloudless_public_ip.edge",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
