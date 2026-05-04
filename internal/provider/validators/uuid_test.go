package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestUUID(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid lowercase", "92e6ed24-8bfa-4737-9409-a1aac994e1f5", false},
		{"valid uppercase", "92E6ED24-8BFA-4737-9409-A1AAC994E1F5", false},
		{"empty", "", false},
		{"missing dashes", "92e6ed248bfa47379409a1aac994e1f5", true},
		{"too short", "92e6ed24-8bfa-4737-9409-a1aac994e1f", true},
		{"non-hex char", "92e6ed24-8bfa-4737-9409-a1aac994e1zz", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			UUID().ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("UUID(%q): error=%v, want %v; diags=%v", c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
