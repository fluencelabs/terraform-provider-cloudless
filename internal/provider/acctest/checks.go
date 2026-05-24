package acctest

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

// CheckDestroy returns a TestCheckFunc that asserts every state resource of
// type tfType has been deleted on the API side, using getByID to fetch.
//
// getByID should return *APIError 404 when the resource is gone; any other
// error is treated as a transient failure and surfaced. The first arg is the
// resource type as it appears in HCL (e.g. "cloudless_ssh_key").
func CheckDestroy(
	_ *client.Client,
	tfType string,
	getByID func(ctx context.Context, id string) error,
) func(*terraform.State) error {
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != tfType {
				continue
			}
			err := getByID(context.Background(), rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("%s %s still exists", tfType, rs.Primary.ID)
			}
			if !client.IsNotFound(err) {
				return fmt.Errorf("%s %s: unexpected error during destroy check: %w", tfType, rs.Primary.ID, err)
			}
		}
		return nil
	}
}
