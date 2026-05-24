package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"
)

func NewClusterDataSource() datasource.DataSource { return &clusterDS{} }

type clusterDS struct{ c *client.Client }

type clusterFilterModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Region   types.String `tfsdk:"region"`
	CityCode types.String `tfsdk:"city_code"`

	DCID             types.String   `tfsdk:"dc_id"`
	DCSlug           types.String   `tfsdk:"dc_slug"`
	DCTier           types.Int64    `tfsdk:"dc_tier"`
	DCCertifications []types.String `tfsdk:"dc_certifications"`
}

func (d *clusterDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (d *clusterDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up exactly one cluster by filter criteria. Errors if more than one matches.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Explicit cluster UUID.",
				Validators:  []validator.String{validators.UUID()},
			},
			"name": schema.StringAttribute{Optional: true, Computed: true},
			"region": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "ISO 3166-1 alpha-2 country code (e.g. DE, PL).",
				Validators:  []validator.String{validators.RegionCode()},
			},
			"city_code": schema.StringAttribute{Optional: true, Computed: true},

			"dc_id": schema.StringAttribute{
				Computed:   true,
				Validators: []validator.String{validators.UUID()},
			},
			"dc_slug":           schema.StringAttribute{Computed: true},
			"dc_tier":           schema.Int64Attribute{Computed: true},
			"dc_certifications": schema.ListAttribute{ElementType: types.StringType, Computed: true},
		},
	}
}

func (d *clusterDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *clusterDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var filter clusterFilterModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &filter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.c.ListEnrichedClusters(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List clusters failed", err.Error())
		return
	}

	matches := []client.EnrichedCluster{}
	for _, c := range all {
		if v := filter.ID.ValueString(); v != "" && c.ID != v {
			continue
		}
		if v := filter.Name.ValueString(); v != "" && c.Name != v {
			continue
		}
		if v := filter.Region.ValueString(); v != "" && c.Region != v {
			continue
		}
		if v := filter.CityCode.ValueString(); v != "" && c.CityCode != v {
			continue
		}
		matches = append(matches, c)
	}

	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("No matching cluster", "no clusters matched the supplied filters")
		return
	case 1:
		// fall through
	default:
		names := []string{}
		for _, m := range matches {
			names = append(names, m.Name+"("+m.ID+")")
		}
		resp.Diagnostics.AddError(
			"Ambiguous cluster filter",
			"more than one cluster matches; narrow the filter. matches: "+joinComma(names),
		)
		return
	}

	c := matches[0]
	out := clusterFilterModel{
		ID:       types.StringValue(c.ID),
		Name:     types.StringValue(c.Name),
		Region:   types.StringValue(c.Region),
		CityCode: types.StringValue(c.CityCode),

		DCID:             types.StringValue(c.DCID),
		DCSlug:           types.StringValue(c.DCSlug),
		DCTier:           types.Int64Value(int64(c.DCTier)),
		DCCertifications: toStringList(c.DCCertifications),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// joinComma is a tiny helper to avoid importing strings just for this.
func joinComma(xs []string) string {
	out := ""
	var outSb122 strings.Builder
	for i, x := range xs {
		if i > 0 {
			outSb122.WriteString(", ")
		}
		outSb122.WriteString(x)
	}
	out += outSb122.String()
	return out
}
