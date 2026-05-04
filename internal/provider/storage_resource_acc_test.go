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
		_, err := c.GetStorage(ctx, id)
		return err
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
data "cloudless_cluster" "main" {
  region = "DE"
}

resource "cloudless_storage" "data" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = %q
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_storage.data", "name", name),
					resource.TestCheckResourceAttr("cloudless_storage.data", "volume_gb", "100"),
					resource.TestCheckResourceAttr("cloudless_storage.data", "role", "DATA"),
				),
			},
			{
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" {
  region = "DE"
}

resource "cloudless_storage" "data" {
  cluster_id   = data.cloudless_cluster.main.id
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
