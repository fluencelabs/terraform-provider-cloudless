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

func storageDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return acctest.CheckDestroy(c, "cloudless_storage", func(ctx context.Context, id string) error {
		got, err := c.GetStorage(ctx, id)
		if err != nil {
			return err
		}
		return acctest.GoneIf(got.Status, nil)
	})
}

func TestAccStorage_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	name := "tf-acc-storage-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             storageDestroy(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "cloudless_clusters" "all" {}

locals {
  # The acceptance account may live in any region; take the first cluster it can see.
  cluster_id = data.cloudless_clusters.all.clusters[0].id
}

resource "cloudless_storage" "data" {
  cluster_id   = local.cluster_id
  name         = %q
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_storage.data", "name", name),
					resource.TestCheckResourceAttr("cloudless_storage.data", "volume_gb", "100"),
				),
			},
			{
				Config: fmt.Sprintf(`
data "cloudless_clusters" "all" {}

locals {
  # The acceptance account may live in any region; take the first cluster it can see.
  cluster_id = data.cloudless_clusters.all.clusters[0].id
}

resource "cloudless_storage" "data" {
  cluster_id   = local.cluster_id
  name         = %q
  storage_type = "NVME"
  volume_gb    = 200
  replicated   = false
}
`, name),
				Check: resource.TestCheckResourceAttr("cloudless_storage.data", "volume_gb", "200"),
			},
			{
				ResourceName:      "cloudless_storage.data",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
