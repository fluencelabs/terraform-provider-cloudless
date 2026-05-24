package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

func NewPublicIPResource() resource.Resource { return &publicIPResource{} }

type publicIPResource struct{ c *client.Client }

type publicIPModel struct {
	ID          types.String `tfsdk:"id"`
	ClusterID   types.String `tfsdk:"cluster_id"`
	Name        types.String `tfsdk:"name"`
	AddressType types.String `tfsdk:"address_type"`
	Address     types.String `tfsdk:"address"`
	Status      types.String `tfsdk:"status"`
	AttachedTo  types.String `tfsdk:"attached_to"`
	UserID      types.String `tfsdk:"user_id"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *publicIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_ip"
}

func (r *publicIPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A public IP address allocated on a Fluence cluster.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cluster_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UUID()},
			},
			"name": schema.StringAttribute{Required: true},
			"address_type": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("V4")},
			},
			"address": schema.StringAttribute{
				Computed:    true,
				Description: "The actual IP address allocated by the provider.",
			},
			"status": schema.StringAttribute{Computed: true},
			"attached_to": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the VM the public IP is attached to, if any.",
			},
			"user_id":    schema.StringAttribute{Computed: true},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *publicIPResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *publicIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan publicIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.CreatePublicIP(ctx, client.CreatePublicIPRequest{
		ClusterID:   plan.ClusterID.ValueString(),
		Name:        plan.Name.ValueString(),
		AddressType: plan.AddressType.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Create public ip failed", err.Error())
		return
	}

	id := out.ID
	out, err = pollUntilReady(ctx,
		func(ctx context.Context) (*client.PublicIP, error) { return r.c.GetPublicIP(ctx, id) },
		func(v *client.PublicIP) string { return v.Status },
		"public ip "+id,
	)
	if err != nil {
		resp.Diagnostics.AddError("Waiting for public ip failed", err.Error())
		return
	}

	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *publicIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state publicIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetPublicIP(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read public ip failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fill(&state, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *publicIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state publicIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	upd := client.UpdatePublicIPRequest{}
	changed := false
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		upd.Name = &v
		changed = true
	}
	var out *client.PublicIP
	if changed {
		got, err := r.c.UpdatePublicIP(ctx, state.ID.ValueString(), upd)
		if err != nil {
			resp.Diagnostics.AddError("Update public ip failed", err.Error())
			return
		}
		out = got
	} else {
		got, err := r.c.GetPublicIP(ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Read public ip failed", err.Error())
			return
		}
		out = got
	}
	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *publicIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state publicIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	deleteAndWait(ctx, &resp.Diagnostics, id,
		r.c.DeletePublicIP,
		func(ctx context.Context) (*client.PublicIP, error) { return r.c.GetPublicIP(ctx, id) },
		func(v *client.PublicIP) string { return v.Status },
		"public ip",
	)
}

func (r *publicIPResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *publicIPResource) fill(m *publicIPModel, p *client.PublicIP) {
	m.ID = types.StringValue(p.ID)
	m.ClusterID = types.StringValue(p.ClusterID)
	m.Name = types.StringValue(p.Name)
	m.AddressType = types.StringValue(p.AddressType)
	m.Address = stringFromPtr(p.Address)
	m.Status = types.StringValue(p.Status)
	m.AttachedTo = stringFromPtr(p.AttachedTo)
	m.UserID = types.StringValue(p.UserID)
	m.CreatedAt = types.StringValue(p.CreatedAt)
}
