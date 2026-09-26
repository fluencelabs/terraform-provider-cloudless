package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"
)

func NewSubnetResource() resource.Resource { return &subnetResource{} }

type subnetResource struct {
	c *client.Client
}

type subnetModel struct {
	ID        types.String `tfsdk:"id"`
	VPCID     types.String `tfsdk:"vpc_id"`
	ClusterID types.String `tfsdk:"cluster_id"`
	Name      types.String `tfsdk:"name"`
	IPv4CIDR  types.String `tfsdk:"ipv4_cidr"`
	IPv6CIDR  types.String `tfsdk:"ipv6_cidr"`
	Egress    types.Bool   `tfsdk:"egress"`
	IsDefault types.Bool   `tfsdk:"is_default"`
	Status    types.String `tfsdk:"status"`
}

func (r *subnetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subnet"
}

func (r *subnetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A subnet inside a Fluence VPC.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"vpc_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description:   "Parent VPC.",
				Validators:    []validator.String{validators.UUID()},
			},
			"cluster_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "Cluster the subnet lives on. If unset, derived from vpc_id's cluster.",
				Validators:  []validator.String{validators.UUID()},
			},
			"name": schema.StringAttribute{Required: true},
			"ipv4_cidr": schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "IPv4 CIDR (e.g. 10.0.0.0/24). A subnet needs at least one of ipv4_cidr and ipv6_cidr.",
				Validators:  []validator.String{validators.CIDR("ipv4")},
			},
			"ipv6_cidr": schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "IPv6 CIDR (e.g. 2001:db8::/64). A subnet needs at least one of ipv4_cidr and ipv6_cidr.",
				Validators:  []validator.String{validators.CIDR("ipv6")},
			},
			"egress": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplaceIfConfigured(),
					boolplanmodifier.UseStateForUnknown(),
				},
				Description: "Outbound internet access; enabled by default. " +
					"An IPv6-only subnet must set it to false — the API does not support egress there.",
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this is the cluster's default subnet — the one a VM lands on with no network_interface block.",
			},
			"status": schema.StringAttribute{Computed: true},
		},
	}
}

// ConfigValidators pins what the API requires and the public spec does not
// say: a subnet is created with at least one CIDR (observed on stage
// 2026-09-11: a create with neither answers 400 "No one cidr provided").
func (r *subnetResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.AtLeastOneOf(path.MatchRoot("ipv4_cidr"), path.MatchRoot("ipv6_cidr")),
		ipv6OnlyHasNoEgress{},
	}
}

// ipv6OnlyHasNoEgress refuses an IPv6-only subnet that leaves egress on: the
// API answers 422 ipv6_egress_unsupported (observed on stage 2026-09-11), and
// egress defaults to enabled, so the refusal would otherwise land at apply.
type ipv6OnlyHasNoEgress struct{}

func (ipv6OnlyHasNoEgress) Description(context.Context) string {
	return "an IPv6-only subnet must set egress = false"
}

func (v ipv6OnlyHasNoEgress) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (ipv6OnlyHasNoEgress) ValidateResource(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var cfg subnetModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ipv6Only := knownString(cfg.IPv6CIDR) && cfg.IPv4CIDR.IsNull()
	egressOn := cfg.Egress.IsNull() || cfg.Egress.ValueBool()
	if ipv6Only && egressOn {
		resp.Diagnostics.AddAttributeError(path.Root("egress"), "Unsupported subnet configuration",
			"an IPv6-only subnet does not support egress: set egress = false, or add ipv4_cidr.")
	}
}

func (r *subnetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *subnetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan subnetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API derives the cluster from the VPC and rejects clusterId in the
	// body; resolve it only to reject a cluster_id that contradicts the VPC.
	resolveClusterID(ctx, r.c, plan.ClusterID, plan.VPCID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.CreateSubnet(ctx, plan.VPCID.ValueString(), client.CreateSubnetRequest{
		Name:     plan.Name.ValueString(),
		IPv4CIDR: nullableString(plan.IPv4CIDR),
		IPv6CIDR: nullableString(plan.IPv6CIDR),
		Egress:   nullableBool(plan.Egress),
	})
	if err != nil {
		resp.Diagnostics.AddError("Create subnet failed", err.Error())
		return
	}

	id := out.ID
	out, err = pollUntilReady(ctx,
		func(ctx context.Context) (*client.Subnet, error) { return r.c.GetSubnet(ctx, id) },
		func(v *client.Subnet) string { return v.Status },
		"subnet "+id,
	)
	if err != nil {
		resp.Diagnostics.AddError("Waiting for subnet failed", err.Error())
		return
	}

	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *subnetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state subnetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetSubnet(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read subnet failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fill(&state, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *subnetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state subnetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Name.Equal(state.Name) {
		updReq := client.UpdateSubnetRequest{Name: nullableString(plan.Name)}
		got, err := r.c.UpdateSubnet(ctx, state.ID.ValueString(), updReq)
		if err != nil {
			resp.Diagnostics.AddError("Update subnet failed", err.Error())
			return
		}
		r.fill(&plan, got)
	} else {
		got, err := r.c.GetSubnet(ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Read subnet failed", err.Error())
			return
		}
		r.fill(&plan, got)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *subnetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state subnetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	deleteAndWait(ctx, &resp.Diagnostics, id,
		r.c.DeleteSubnet,
		func(ctx context.Context) (*client.Subnet, error) { return r.c.GetSubnet(ctx, id) },
		func(v *client.Subnet) string { return v.Status },
		"subnet",
	)
}

func (r *subnetResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *subnetResource) fill(m *subnetModel, s *client.Subnet) {
	m.ID = types.StringValue(s.ID)
	m.VPCID = types.StringValue(s.VPCID)
	m.ClusterID = types.StringValue(s.ClusterID)
	m.Name = types.StringValue(s.Name)
	m.IPv4CIDR = stringFromPtr(s.IPv4CIDR)
	m.IPv6CIDR = stringFromPtr(s.IPv6CIDR)
	m.Egress = types.BoolValue(s.Egress)
	m.IsDefault = types.BoolValue(s.IsDefault)
	m.Status = types.StringValue(s.Status)
}
