package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
	Status    types.String `tfsdk:"status"`
	UserID    types.String `tfsdk:"user_id"`
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
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
				Description:   "Cluster the subnet lives on. If unset, derived from vpc_id's cluster.",
				Validators:    []validator.String{validators.UUID()},
			},
			"name": schema.StringAttribute{Required: true},
			"ipv4_cidr": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
				Description:   "Optional IPv4 CIDR (e.g. 10.0.0.0/24).",
				Validators:    []validator.String{validators.CIDR("ipv4")},
			},
			"ipv6_cidr": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
				Description:   "Optional IPv6 CIDR (e.g. 2001:db8::/64).",
				Validators:    []validator.String{validators.CIDR("ipv6")},
			},
			"status":  schema.StringAttribute{Computed: true},
			"user_id": schema.StringAttribute{Computed: true},
		},
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

	clusterID := resolveClusterID(ctx, r.c, plan.ClusterID, plan.VPCID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.CreateSubnet(ctx, plan.VPCID.ValueString(), client.CreateSubnetRequest{
		ClusterID: clusterID,
		Name:      plan.Name.ValueString(),
		IPv4CIDR:  nullableString(plan.IPv4CIDR),
		IPv6CIDR:  nullableString(plan.IPv6CIDR),
	})
	if err != nil {
		resp.Diagnostics.AddError("Create subnet failed", err.Error())
		return
	}

	id := out.ID
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetSubnet(ctx, id)
		if err != nil {
			return err
		}
		out = got
		if isReady(got.Status) {
			return errStopPolling
		}
		if terminalFailure(got.Status) {
			return fmt.Errorf("subnet %s entered terminal status %q", id, got.Status)
		}
		return nil
	}); err != nil {
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
	if err := r.c.DeleteSubnet(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete subnet failed", err.Error())
		return
	}
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetSubnet(ctx, id)
		if err != nil {
			if client.IsNotFound(err) {
				return errStopPolling
			}
			return err
		}
		if isRemoved(got.Status) {
			return errStopPolling
		}
		if terminalFailure(got.Status) {
			return fmt.Errorf("subnet %s entered terminal status %q during delete", id, got.Status)
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for subnet deletion failed", err.Error())
	}
}

func (r *subnetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *subnetResource) fill(m *subnetModel, s *client.Subnet) {
	m.ID = types.StringValue(s.ID)
	m.VPCID = types.StringValue(s.VPCID)
	m.ClusterID = types.StringValue(s.ClusterID)
	m.Name = types.StringValue(s.Name)
	m.IPv4CIDR = stringFromPtr(s.IPv4CIDR)
	m.IPv6CIDR = stringFromPtr(s.IPv6CIDR)
	m.Status = types.StringValue(s.Status)
	m.UserID = types.StringValue(s.UserID)
}
