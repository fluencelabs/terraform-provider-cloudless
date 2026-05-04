package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"
)

func NewClustersDataSource() datasource.DataSource { return &clustersDS{} }

type clustersDS struct{ c *client.Client }

type clustersListModel struct {
	Regions   []types.String `tfsdk:"regions"`
	CityCodes []types.String `tfsdk:"city_codes"`
	Names     []types.String `tfsdk:"names"`
	Clusters  []clusterModel `tfsdk:"clusters"`
}

type clusterModel struct {
	ID               types.String   `tfsdk:"id"`
	Name             types.String   `tfsdk:"name"`
	Region           types.String   `tfsdk:"region"`
	CityCode         types.String   `tfsdk:"city_code"`
	DCID             types.String   `tfsdk:"dc_id"`
	DCSlug           types.String   `tfsdk:"dc_slug"`
	DCTier           types.Int64    `tfsdk:"dc_tier"`
	DCCertifications []types.String `tfsdk:"dc_certifications"`
}

func (d *clustersDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_clusters"
}

func (d *clustersDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List clusters available to the calling account. Optionally filter by region (country) / city / name.",
		Attributes: map[string]schema.Attribute{
			"regions": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "ISO country codes; AND-composed with city_codes/names.",
				Validators:  []validator.List{listvalidator.ValueStringsAre(validators.RegionCode())},
			},
			"city_codes": schema.ListAttribute{Optional: true, ElementType: types.StringType},
			"names":      schema.ListAttribute{Optional: true, ElementType: types.StringType},
			"clusters": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                schema.StringAttribute{Computed: true},
						"name":              schema.StringAttribute{Computed: true},
						"region":            schema.StringAttribute{Computed: true},
						"city_code":         schema.StringAttribute{Computed: true},
						"dc_id":             schema.StringAttribute{Computed: true},
						"dc_slug":           schema.StringAttribute{Computed: true},
						"dc_tier":           schema.Int64Attribute{Computed: true},
						"dc_certifications": schema.ListAttribute{ElementType: types.StringType, Computed: true},
					},
				},
			},
		},
	}
}

func (d *clustersDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *clustersDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var filter clustersListModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &filter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.c.ListEnrichedClusters(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List clusters failed", err.Error())
		return
	}

	regions := stringListSet(filter.Regions)
	cities := stringListSet(filter.CityCodes)
	names := stringListSet(filter.Names)

	out := clustersListModel{
		Regions:   filter.Regions,
		CityCodes: filter.CityCodes,
		Names:     filter.Names,
		Clusters:  []clusterModel{},
	}
	for _, c := range all {
		if !setMatch(regions, c.Region) {
			continue
		}
		if !setMatch(cities, c.CityCode) {
			continue
		}
		if !setMatch(names, c.Name) {
			continue
		}
		out.Clusters = append(out.Clusters, clusterModel{
			ID:               types.StringValue(c.ID),
			Name:             types.StringValue(c.Name),
			Region:           types.StringValue(c.Region),
			CityCode:         types.StringValue(c.CityCode),
			DCID:             types.StringValue(c.DCID),
			DCSlug:           types.StringValue(c.DCSlug),
			DCTier:           types.Int64Value(int64(c.DCTier)),
			DCCertifications: toStringList(c.DCCertifications),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// stringListSet builds a small lookup set; nil/empty input is "match all".
func stringListSet(in []types.String) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := map[string]struct{}{}
	for _, v := range in {
		if !v.IsNull() {
			out[v.ValueString()] = struct{}{}
		}
	}
	return out
}

func setMatch(set map[string]struct{}, v string) bool {
	if set == nil {
		return true
	}
	_, ok := set[v]
	return ok
}
