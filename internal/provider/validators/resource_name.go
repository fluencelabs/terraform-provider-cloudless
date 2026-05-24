package validators

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// maxResourceNameLen is the Fluence API's name length limit (vodopad
// is_valid_name).
const maxResourceNameLen = 25

var resourceNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

type resourceNameValidator struct{}

// ResourceName mirrors the Fluence API's name rule (vodopad is_valid_name):
// lowercase letters, digits and hyphens only, no leading/trailing hyphen, at
// most 25 characters. It rejects invalid names at plan time instead of letting
// them fail at apply. Empty strings pass — pair with Required:true on the
// schema to enforce presence.
func ResourceName() validator.String { return resourceNameValidator{} }

func (resourceNameValidator) Description(_ context.Context) string {
	return "value must be 1-25 chars of [a-z0-9-], not starting or ending with a hyphen"
}
func (v resourceNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (resourceNameValidator) ValidateString(
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
	if len(v) > maxResourceNameLen {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid name",
			"name must be at most 25 characters, got "+v)
		return
	}
	if v[0] == '-' || v[len(v)-1] == '-' || !resourceNamePattern.MatchString(v) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid name",
			"name may contain only lowercase letters, digits and hyphens, "+
				"and cannot start or end with a hyphen, got: "+v)
	}
}
