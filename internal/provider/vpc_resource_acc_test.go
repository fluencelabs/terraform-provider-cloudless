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

func vpcDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return acctest.CheckDestroy(c, "cloudless_vpc", func(ctx context.Context, id string) error {
		_, err := c.GetVPC(ctx, id)
		return err
	})
}

func TestAccVPC_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	name := "tf-acc-vpc-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)
	renamed := name + "-renamed"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             vpcDestroy(),
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
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vpc.main", "name", name),
					resource.TestCheckResourceAttrSet("cloudless_vpc.main", "id"),
				),
			},
			{
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" {
  region = "DE"
}

resource "cloudless_vpc" "main" {
  cluster_id = data.cloudless_cluster.main.id
  name       = %q
}
`, renamed),
				Check: resource.TestCheckResourceAttr("cloudless_vpc.main", "name", renamed),
			},
			{
				ResourceName:      "cloudless_vpc.main",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
