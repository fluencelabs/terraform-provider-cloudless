package validators

import (
	"context"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// portRangeParts is the number of "min-max" fields in a port range spec.
const portRangeParts = 2

// PortSpec accepts: "" (empty), "all", a port number 0-65535, or a "min-max"
// range with min ≤ max and both in 0-65535.
func PortSpec() validator.String { return portSpecValidator{} }

type portSpecValidator struct{}

func (portSpecValidator) Description(_ context.Context) string {
	return `value must be "all", a port number, or a "min-max" range`
}
func (v portSpecValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (portSpecValidator) ValidateString(
	_ context.Context,
	req validator.StringRequest,
	resp *validator.StringResponse,
) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if val == "" || val == "all" {
		return
	}
	if !strings.Contains(val, "-") {
		_, err := strconv.ParseUint(val, 10, 16)
		if err != nil {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid port", "expected 0-65535, got "+val)
		}
		return
	}
	parts := strings.Split(val, "-")
	if len(parts) != portRangeParts {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid port range", `expected "min-max"`)
		return
	}
	mn, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid port range", "min: "+parts[0]+" not a valid port")
		return
	}
	mx, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid port range", "max: "+parts[1]+" not a valid port")
		return
	}
	if mn > mx {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid port range", "min > max")
	}
}
