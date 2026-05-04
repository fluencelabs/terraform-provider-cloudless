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

func NewVPCResource() resource.Resource { return &vpcResource{} }

type vpcResource struct {
	c *client.Client
}

type vpcModel struct {
	ID             types.String `tfsdk:"id"`
	ClusterID      types.String `tfsdk:"cluster_id"`
	Name           types.String `tfsdk:"name"`
	EnableExternal types.Bool   `tfsdk:"enable_external"`
	Status         types.String `tfsdk:"status"`
	SubnetsCount   types.Int64  `tfsdk:"subnets_count"`
	UserID         types.String `tfsdk:"user_id"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (r *vpcResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc"
}

func (r *vpcResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A virtual private cloud on a Fluence cluster.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cluster_id": schema.StringAttribute{
				Required:      true,
				Description:   "Cluster (UUID) the VPC belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UUID()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable VPC name.",
			},
			"enable_external": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true the VPC has external network connectivity.",
			},
			"status":        schema.StringAttribute{Computed: true},
			"subnets_count": schema.Int64Attribute{Computed: true},
			"user_id":       schema.StringAttribute{Computed: true},
			"created_at":    schema.StringAttribute{Computed: true},
		},
	}
}

func (r *vpcResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *vpcResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpcModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.CreateVPC(ctx, client.CreateVPCRequest{
		ClusterID:      plan.ClusterID.ValueString(),
		Name:           plan.Name.ValueString(),
		EnableExternal: nullableBool(plan.EnableExternal),
	})
	if err != nil {
		resp.Diagnostics.AddError("Create VPC failed", err.Error())
		return
	}

	id := out.ID
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetVPC(ctx, id)
		if err != nil {
			return err
		}
		out = got
		if isReady(got.Status) {
			return errStopPolling
		}
		if terminalFailure(got.Status) {
			return fmt.Errorf("vpc %s entered terminal status %q", id, got.Status)
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for VPC failed", err.Error())
		return
	}

	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpcResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpcModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.GetVPC(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read VPC failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fill(&state, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vpcResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vpcModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updReq := client.UpdateVPCRequest{}
	changed := false
	if !plan.Name.Equal(state.Name) {
		updReq.Name = nullableString(plan.Name)
		changed = true
	}
	if !plan.EnableExternal.Equal(state.EnableExternal) {
		updReq.EnableExternal = nullableBool(plan.EnableExternal)
		changed = true
	}

	var out *client.VPC
	if changed {
		got, err := r.c.UpdateVPC(ctx, state.ID.ValueString(), updReq)
		if err != nil {
			resp.Diagnostics.AddError("Update VPC failed", err.Error())
			return
		}
		out = got
	} else {
		got, err := r.c.GetVPC(ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Read VPC failed", err.Error())
			return
		}
		out = got
	}

	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpcResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpcModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.c.DeleteVPC(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete VPC failed", err.Error())
		return
	}

	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetVPC(ctx, id)
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
			return fmt.Errorf("vpc %s entered terminal status %q during delete", id, got.Status)
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for VPC deletion failed", err.Error())
	}
}

func (r *vpcResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *vpcResource) fill(m *vpcModel, v *client.VPC) {
	m.ID = types.StringValue(v.ID)
	m.ClusterID = types.StringValue(v.ClusterID)
	m.Name = types.StringValue(v.Name)
	m.EnableExternal = boolFromPtr(v.EnableExternal)
	m.Status = types.StringValue(v.Status)
	m.SubnetsCount = types.Int64Value(int64(v.SubnetsCount))
	m.UserID = types.StringValue(v.UserID)
	m.CreatedAt = types.StringValue(v.CreatedAt)
}
