package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewVMConfigurationDataSource() datasource.DataSource { return &vmConfigDS{} }

type vmConfigDS struct{ c *client.Client }

func (d *vmConfigDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_configuration"
}

func (d *vmConfigDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up exactly one VM configuration (CPU/RAM preset) by filter criteria. " +
			"Errors if more than one matches — this is the readable way to name a configuration_id.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Optional: true, Computed: true, Description: "Explicit configuration id."},
			"slug": schema.StringAttribute{Optional: true, Computed: true},
			"name": schema.StringAttribute{Optional: true, Computed: true},
			"vcpu": schema.Int64Attribute{Optional: true, Computed: true},
			"ram_gb": schema.Int64Attribute{
				Optional: true, Computed: true,
			},
			"dedicated":    schema.BoolAttribute{Optional: true, Computed: true},
			"cpu_families": schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"tags":         schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"description":  schema.StringAttribute{Computed: true},
		},
	}
}

func (d *vmConfigDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func vmConfigMatches(filter vmConfigModel, c client.VMConfiguration) bool {
	switch {
	case filter.ID.ValueString() != "" && c.ID != filter.ID.ValueString():
	case filter.Slug.ValueString() != "" && c.Slug != filter.Slug.ValueString():
	case filter.Name.ValueString() != "" && c.Name != filter.Name.ValueString():
	case !filter.VCPU.IsNull() && int64(c.VCPU) != filter.VCPU.ValueInt64():
	case !filter.RAMGb.IsNull() && int64(c.RAMGb) != filter.RAMGb.ValueInt64():
	case !filter.Dedicated.IsNull() && c.Dedicated != filter.Dedicated.ValueBool():
	default:
		return true
	}
	return false
}

func (d *vmConfigDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var filter vmConfigModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &filter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.c.ListVMConfigurations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List VM configurations failed", err.Error())
		return
	}

	matches := []client.VMConfiguration{}
	for _, c := range all {
		if vmConfigMatches(filter, c) {
			matches = append(matches, c)
		}
	}

	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, fmt.Sprintf("%s (%d vCPU, %d GB)", m.Slug, m.VCPU, m.RAMGb))
	}
	if !exactlyOne(len(matches), "VM configuration", names, &resp.Diagnostics) {
		return
	}

	c := matches[0]
	out := vmConfigModel{
		ID:          types.StringValue(c.ID),
		Slug:        types.StringValue(c.Slug),
		Name:        types.StringValue(c.Name),
		VCPU:        types.Int64Value(int64(c.VCPU)),
		RAMGb:       types.Int64Value(int64(c.RAMGb)),
		Dedicated:   types.BoolValue(c.Dedicated),
		CPUFamilies: toStringList(c.CPUFamilies),
		Tags:        toStringList(c.Tags),
		Description: types.StringValue(c.Description),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}
