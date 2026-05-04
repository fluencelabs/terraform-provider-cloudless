package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCIDR(t *testing.T) {
	cases := []struct {
		name    string
		family  string
		value   string
		wantErr bool
	}{
		{"v4 ok", "ipv4", "10.0.0.0/24", false},
		{"v4 with full bits", "ipv4", "0.0.0.0/0", false},
		{"v4 not v6", "ipv4", "2001:db8::/64", true},
		{"v6 ok", "ipv6", "2001:db8::/64", false},
		{"v6 not v4", "ipv6", "10.0.0.0/24", true},
		{"any v4", "any", "10.0.0.0/24", false},
		{"any v6", "any", "2001:db8::/64", false},
		{"missing prefix", "any", "10.0.0.0", true},
		{"bad prefix len", "ipv4", "10.0.0.0/33", true},
		{"empty", "any", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			CIDR(c.family).ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("CIDR(%q,%q): error=%v want %v; diags=%v", c.family, c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
