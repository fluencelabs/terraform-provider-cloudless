package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestRegionCode(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty", "", false},
		{"DE", "DE", false},
		{"PL", "PL", false},
		{"lower", "de", true},
		{"single char", "D", true},
		{"three chars", "DEU", true},
		{"digits", "12", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			RegionCode().ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("RegionCode(%q): error=%v want %v; diags=%v", c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
