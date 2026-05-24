package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewVMConfigurationsDataSource() datasource.DataSource { return &vmConfigsDS{} }

type vmConfigsDS struct {
	c *client.Client
}

type vmConfigModel struct {
	ID          types.String   `tfsdk:"id"`
	Slug        types.String   `tfsdk:"slug"`
	Name        types.String   `tfsdk:"name"`
	VCPU        types.Int64    `tfsdk:"vcpu"`
	RAMGb       types.Int64    `tfsdk:"ram_gb"`
	Dedicated   types.Bool     `tfsdk:"dedicated"`
	CPUFamilies []types.String `tfsdk:"cpu_families"`
	Tags        []types.String `tfsdk:"tags"`
	Description types.String   `tfsdk:"description"`
}

type vmConfigsModel struct {
	Configurations []vmConfigModel `tfsdk:"configurations"`
}

func (d *vmConfigsDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_configurations"
}

func (d *vmConfigsDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "All VM configurations (CPU/RAM presets) available for VM creation.",
		Attributes: map[string]schema.Attribute{
			"configurations": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":           schema.StringAttribute{Computed: true},
						"slug":         schema.StringAttribute{Computed: true},
						"name":         schema.StringAttribute{Computed: true},
						"vcpu":         schema.Int64Attribute{Computed: true},
						"ram_gb":       schema.Int64Attribute{Computed: true},
						"dedicated":    schema.BoolAttribute{Computed: true},
						"cpu_families": schema.ListAttribute{ElementType: types.StringType, Computed: true},
						"tags":         schema.ListAttribute{ElementType: types.StringType, Computed: true},
						"description":  schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *vmConfigsDS) Configure(
	_ context.Context,
	req datasource.ConfigureRequest,
	resp *datasource.ConfigureResponse,
) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *vmConfigsDS) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	out, err := d.c.ListVMConfigurations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List VM configurations failed", err.Error())
		return
	}
	model := vmConfigsModel{Configurations: make([]vmConfigModel, 0, len(out))}
	for _, c := range out {
		model.Configurations = append(model.Configurations, vmConfigModel{
			ID:          types.StringValue(c.ID),
			Slug:        types.StringValue(c.Slug),
			Name:        types.StringValue(c.Name),
			VCPU:        types.Int64Value(int64(c.VCPU)),
			RAMGb:       types.Int64Value(int64(c.RAMGb)),
			Dedicated:   types.BoolValue(c.Dedicated),
			CPUFamilies: toStringList(c.CPUFamilies),
			Tags:        toStringList(c.Tags),
			Description: types.StringValue(c.Description),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
