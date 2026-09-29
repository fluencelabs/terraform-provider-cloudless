package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewDefaultImagesDataSource() datasource.DataSource { return &defaultImagesDS{} }

type defaultImagesDS struct {
	c *client.Client
}

type defaultImageModel struct {
	ID           types.String `tfsdk:"id"`
	Slug         types.String `tfsdk:"slug"`
	Name         types.String `tfsdk:"name"`
	Distribution types.String `tfsdk:"distribution"`
	BootMode     types.String `tfsdk:"boot_mode"`
	IsDefault    types.Bool   `tfsdk:"is_default"`
	Username     types.String `tfsdk:"username"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

type defaultImagesModel struct {
	Images []defaultImageModel `tfsdk:"images"`
}

func (d *defaultImagesDS) Metadata(
	_ context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_default_images"
}

func (d *defaultImagesDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "All Fluence-curated default OS images suitable for boot disks.",
		Attributes: map[string]schema.Attribute{
			"images": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":           schema.StringAttribute{Computed: true},
						"slug":         schema.StringAttribute{Computed: true},
						"name":         schema.StringAttribute{Computed: true},
						"distribution": schema.StringAttribute{Computed: true},
						"boot_mode":    schema.StringAttribute{Computed: true},
						"is_default":   schema.BoolAttribute{Computed: true},
						"username":     schema.StringAttribute{Computed: true},
						"created_at":   schema.StringAttribute{Computed: true},
						"updated_at":   schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *defaultImagesDS) Configure(
	_ context.Context,
	req datasource.ConfigureRequest,
	resp *datasource.ConfigureResponse,
) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *defaultImagesDS) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	out, err := d.c.ListDefaultImages(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List default images failed", err.Error())
		return
	}
	model := defaultImagesModel{Images: make([]defaultImageModel, 0, len(out))}
	for _, i := range out {
		model.Images = append(model.Images, defaultImageModel{
			ID:           types.StringValue(i.ID),
			Slug:         types.StringValue(i.Slug),
			Name:         types.StringValue(i.Name),
			Distribution: types.StringValue(i.Distribution),
			Username:     types.StringValue(i.Username),
			CreatedAt:    types.StringValue(i.CreatedAt),
			UpdatedAt:    types.StringValue(i.UpdatedAt),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
