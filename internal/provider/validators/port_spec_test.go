package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPortSpec(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty", "", false},
		{"all", "all", false},
		{"single", "443", false},
		{"range", "8000-8100", false},
		{"single 0", "0", false},
		{"single 65535", "65535", false},
		{"single 65536", "65536", true},
		{"negative", "-1", true},
		{"reverse range", "100-50", true},
		{"non-numeric", "abc", true},
		{"trailing dash", "100-", true},
		{"extra dash", "1-2-3", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			PortSpec().ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("PortSpec(%q): error=%v want %v; diags=%v", c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
