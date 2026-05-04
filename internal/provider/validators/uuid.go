package validators

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type uuidValidator struct{}

// UUID returns a validator that accepts canonical 8-4-4-4-12 hex UUIDs.
// Empty strings pass — wire that with Required:true on the schema if needed.
func UUID() validator.String { return uuidValidator{} }

func (uuidValidator) Description(_ context.Context) string {
	return "value must be a UUID in 8-4-4-4-12 hex format"
}
func (v uuidValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (uuidValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	v := req.ConfigValue.ValueString()
	if v == "" {
		return
	}
	if !uuidPattern.MatchString(v) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid UUID", "expected an 8-4-4-4-12 hex UUID, got: "+v)
	}
}
