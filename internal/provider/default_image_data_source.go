package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewDefaultImageDataSource() datasource.DataSource { return &defaultImageDS{} }

type defaultImageDS struct{ c *client.Client }

func (d *defaultImageDS) Metadata(
	_ context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_default_image"
}

func (d *defaultImageDS) Schema(
	_ context.Context,
	_ datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Look up exactly one catalog OS image by filter criteria (usually slug). " +
			"Errors if more than one matches — this is the readable way to name a boot_disk image_id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Explicit image id.",
			},
			"slug":         schema.StringAttribute{Optional: true, Computed: true},
			"name":         schema.StringAttribute{Optional: true, Computed: true},
			"distribution": schema.StringAttribute{Optional: true, Computed: true},
			"boot_mode": schema.StringAttribute{
				Computed:    true,
				Description: "Firmware the image boots with (BIOS or EFI).",
			},
			"is_default": schema.BoolAttribute{Computed: true},
			"username": schema.StringAttribute{
				Computed:    true,
				Description: "The account the image ships with — who to ssh in as.",
			},
			"created_at": schema.StringAttribute{Computed: true},
			"updated_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *defaultImageDS) Configure(
	_ context.Context,
	req datasource.ConfigureRequest,
	resp *datasource.ConfigureResponse,
) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *defaultImageDS) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var filter defaultImageModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &filter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.c.ListDefaultImages(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List default images failed", err.Error())
		return
	}

	matches := []client.DefaultImage{}
	names := []string{}
	for _, im := range all {
		if v := filter.ID.ValueString(); v != "" && im.ID != v {
			continue
		}
		if v := filter.Slug.ValueString(); v != "" && im.Slug != v {
			continue
		}
		if v := filter.Name.ValueString(); v != "" && im.Name != v {
			continue
		}
		if v := filter.Distribution.ValueString(); v != "" && im.Distribution != v {
			continue
		}
		matches = append(matches, im)
		names = append(names, im.Slug)
	}
	if !exactlyOne(len(matches), "image", names, &resp.Diagnostics) {
		return
	}

	im := matches[0]
	out := defaultImageModel{
		ID:           types.StringValue(im.ID),
		Slug:         types.StringValue(im.Slug),
		Name:         types.StringValue(im.Name),
		Distribution: types.StringValue(im.Distribution),
		BootMode:     types.StringValue(im.BootMode),
		IsDefault:    types.BoolValue(im.IsDefault),
		Username:     types.StringValue(im.Username),
		CreatedAt:    types.StringValue(im.CreatedAt),
		UpdatedAt:    types.StringValue(im.UpdatedAt),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}
