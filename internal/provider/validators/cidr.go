package validators

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// CIDR returns a validator that accepts a CIDR-formatted IP block.
// family is "ipv4", "ipv6", or "any".
func CIDR(family string) validator.String {
	return cidrValidator{family: family}
}

type cidrValidator struct{ family string }

func (v cidrValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be a CIDR block (%s)", v.family)
}
func (v cidrValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v cidrValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if val == "" {
		return
	}
	ip, _, err := net.ParseCIDR(val)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR", err.Error())
		return
	}
	is4 := ip.To4() != nil && !strings.Contains(val, ":")
	switch v.family {
	case "ipv4":
		if !is4 {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR family", "expected IPv4 CIDR, got "+val)
		}
	case "ipv6":
		if is4 {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR family", "expected IPv6 CIDR, got "+val)
		}
	case "any":
	default:
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid validator config", "unknown CIDR family: "+v.family)
	}
}
