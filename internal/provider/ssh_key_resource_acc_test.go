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

func sshKeyDestroy() func(*terraform.State) error {
	c := acctest.RealClient()
	return acctest.CheckDestroy(c, "cloudless_ssh_key", func(ctx context.Context, id string) error {
		_, err := c.GetSSHKey(ctx, id)
		return err
	})
}

func TestAccSSHKey_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	name := "tf-acc-ssh-" + tfacctest.RandStringFromCharSet(8, tfacctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             sshKeyDestroy(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "cloudless_ssh_key" "me" {
  name       = %q
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1DjqOOxINs78oU3f7PJXIyq9uiNocNVhXNx tf-acc"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_ssh_key.me", "name", name),
					resource.TestCheckResourceAttrSet("cloudless_ssh_key.me", "fingerprint"),
				),
			},
			{
				ResourceName:      "cloudless_ssh_key.me",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
