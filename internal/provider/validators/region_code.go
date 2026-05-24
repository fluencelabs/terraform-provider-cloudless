package validators

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// RegionCode validates an ISO 3166-1 alpha-2 country code (uppercase 2 letters).
func RegionCode() validator.String { return regionCodeValidator{} }

var regionPattern = regexp.MustCompile(`^[A-Z]{2}$`)

type regionCodeValidator struct{}

func (regionCodeValidator) Description(_ context.Context) string {
	return "value must be an ISO 3166-1 alpha-2 country code (e.g. DE, PL)"
}
func (v regionCodeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (regionCodeValidator) ValidateString(
	_ context.Context,
	req validator.StringRequest,
	resp *validator.StringResponse,
) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	v := req.ConfigValue.ValueString()
	if v == "" {
		return
	}
	if !regionPattern.MatchString(v) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid region code",
			"expected 2 uppercase letters (ISO 3166-1 alpha-2), got "+v,
		)
	}
}
