package validators_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"
)

func TestResourceName(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty passes (Required handles presence)", "", false},
		{"simple", "igor-vpc", false},
		{"with digits", "valid-123", false},
		{"exactly 25 chars", strings.Repeat("a", 25), false},
		{"boot name fits", "tf-cloudless-vm-boot", false},
		{"26 chars too long", strings.Repeat("a", 26), true},
		{"old overflowing boot name", "tf-cloudless-infra-vm-boot", true},
		{"uppercase", "Has-Upper", true},
		{"leading hyphen", "-leading", true},
		{"trailing hyphen", "trailing-", true},
		{"underscore", "under_score", true},
		{"space", "with space", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			validators.ResourceName().ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("ResourceName(%q): error=%v want %v; diags=%v", c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
