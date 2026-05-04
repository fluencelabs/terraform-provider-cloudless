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

func subnetDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return acctest.CheckDestroy(c, "cloudless_subnet", func(ctx context.Context, id string) error {
		_, err := c.GetSubnet(ctx, id)
		return err
	})
}

func TestAccSubnet_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	vpcName := "tf-acc-vpc-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	subnetName := "tf-acc-subnet-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             subnetDestroy(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" {
  region = "DE"
}

resource "cloudless_vpc" "main" {
  cluster_id = data.cloudless_cluster.main.id
  name       = %q
}

resource "cloudless_subnet" "s" {
  vpc_id = cloudless_vpc.main.id
  name   = %q
}
`, vpcName, subnetName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_subnet.s", "name", subnetName),
					// cluster_id should be derived from the parent VPC.
					resource.TestCheckResourceAttrPair(
						"cloudless_subnet.s", "cluster_id",
						"cloudless_vpc.main", "cluster_id",
					),
				),
			},
			{
				ResourceName:      "cloudless_subnet.s",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
