package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewSubnetsDataSource() datasource.DataSource { return &subnetsDS{} }

type subnetsDS struct{ c *client.Client }

type subnetsListModel struct {
	ClusterIDs []types.String    `tfsdk:"cluster_ids"`
	VPCIDs     []types.String    `tfsdk:"vpc_ids"`
	OnlyUsable types.Bool        `tfsdk:"only_usable"`
	Subnets    []subnetListEntry `tfsdk:"subnets"`
}

type subnetListEntry struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	VPCID     types.String `tfsdk:"vpc_id"`
	ClusterID types.String `tfsdk:"cluster_id"`
	IPv4CIDR  types.String `tfsdk:"ipv4_cidr"`
	IPv6CIDR  types.String `tfsdk:"ipv6_cidr"`
	Egress    types.Bool   `tfsdk:"egress"`
	IsDefault types.Bool   `tfsdk:"is_default"`
	Status    types.String `tfsdk:"status"`
}

func (d *subnetsDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subnets"
}

func (d *subnetsDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List the subnets the calling account can see. A VM needs no VPC or subnet of its own: " +
			"the account already has a default subnet per cluster, and this data source is how to point at it " +
			"without hard-coding an id.",
		Attributes: map[string]schema.Attribute{
			"cluster_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Keep only subnets in these clusters; AND-composed with vpc_ids.",
			},
			"vpc_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Keep only subnets in these VPCs; AND-composed with cluster_ids.",
			},
			"only_usable": schema.BoolAttribute{
				Optional: true,
				Description: "Drop subnets that are gone or still building, keeping the ones a VM can be placed on. " +
					"Defaults to true.",
			},
			"subnets": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":         schema.StringAttribute{Computed: true},
						"name":       schema.StringAttribute{Computed: true},
						"vpc_id":     schema.StringAttribute{Computed: true},
						"cluster_id": schema.StringAttribute{Computed: true},
						"ipv4_cidr":  schema.StringAttribute{Computed: true},
						"ipv6_cidr":  schema.StringAttribute{Computed: true},
						"egress":     schema.BoolAttribute{Computed: true},
						"is_default": schema.BoolAttribute{
							Computed:    true,
							Description: "The cluster's default subnet — where a VM lands with no network_interface block.",
						},
						"status": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *subnetsDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *subnetsDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var filter subnetsListModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &filter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.c.ListSubnets(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List subnets failed", err.Error())
		return
	}

	clusters := stringListSet(filter.ClusterIDs)
	vpcs := stringListSet(filter.VPCIDs)
	usableOnly := filter.OnlyUsable.IsNull() || filter.OnlyUsable.ValueBool()

	out := subnetsListModel{
		ClusterIDs: filter.ClusterIDs,
		VPCIDs:     filter.VPCIDs,
		OnlyUsable: filter.OnlyUsable,
		Subnets:    []subnetListEntry{},
	}
	for _, s := range all {
		if !setMatch(clusters, s.ClusterID) || !setMatch(vpcs, s.VPCID) {
			continue
		}
		if usableOnly && s.Status != statusReady {
			continue
		}
		out.Subnets = append(out.Subnets, subnetListEntry{
			ID:        types.StringValue(s.ID),
			Name:      types.StringValue(s.Name),
			VPCID:     types.StringValue(s.VPCID),
			ClusterID: types.StringValue(s.ClusterID),
			IPv4CIDR:  stringFromPtr(s.IPv4CIDR),
			IPv6CIDR:  stringFromPtr(s.IPv6CIDR),
			Egress:    types.BoolValue(s.Egress),
			IsDefault: types.BoolValue(s.IsDefault),
			Status:    types.StringValue(s.Status),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}
