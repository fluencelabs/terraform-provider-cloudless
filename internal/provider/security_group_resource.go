package provider

import (
	"context"
	"fmt"

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

func NewSecurityGroupResource() resource.Resource { return &sgResource{} }

type sgResource struct{ c *client.Client }

type sgModel struct {
	ID          types.String `tfsdk:"id"`
	ClusterID   types.String `tfsdk:"cluster_id"`
	Name        types.String `tfsdk:"name"`
	IngressMode types.String `tfsdk:"ingress_mode"`
	EgressMode  types.String `tfsdk:"egress_mode"`
	Ingress     []sgRule     `tfsdk:"ingress"`
	Egress      []sgRule     `tfsdk:"egress"`
	Status      types.String `tfsdk:"status"`
	UserID      types.String `tfsdk:"user_id"`
	VPCID       types.String `tfsdk:"vpc_id"`
}

type sgRule struct {
	Protocol        types.String `tfsdk:"protocol"`
	Ports           types.String `tfsdk:"ports"`
	CIDR            types.String `tfsdk:"cidr"`
	SecurityGroupID types.String `tfsdk:"security_group_id"`
	Type            types.String `tfsdk:"type"`
}

func (r *sgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_group"
}

func sgRuleBlock() schema.NestedBlockObject {
	return schema.NestedBlockObject{
		Attributes: map[string]schema.Attribute{
			"protocol": schema.StringAttribute{
				Required:    true,
				Description: "One of `tcp`, `udp`, `icmp`, `all`.",
				Validators:  []validator.String{stringvalidator.OneOf("tcp", "udp", "icmp", "all")},
			},
			"ports": schema.StringAttribute{
				Optional:    true,
				Description: "Single port (e.g. `443`), inclusive range (e.g. `8000-8100`), or `all`. Required for tcp/udp; must be empty for icmp/all.",
				Validators:  []validator.String{validators.PortSpec()},
			},
			"cidr": schema.StringAttribute{
				Optional:    true,
				Description: "Remote address as a CIDR block (e.g. `10.0.0.0/24`). Mutually exclusive with `security_group_id`.",
				Validators:  []validator.String{validators.CIDR("any")},
			},
			"security_group_id": schema.StringAttribute{
				Optional:    true,
				Description: "Remote security group's UUID — match traffic from any interface bound to this SG. Mutually exclusive with `cidr`.",
				Validators:  []validator.String{validators.UUID()},
			},
			"type": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Address family: `ipv4` (default) or `ipv6`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:    []validator.String{stringvalidator.OneOf("ipv4", "ipv6")},
			},
		},
	}
}

func (r *sgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A security group on a Fluence cluster. Per-direction mode controls how rule blocks are interpreted.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"cluster_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UUID()},
			},
			"name": schema.StringAttribute{Required: true},
			"ingress_mode": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: "\"allow_all\" (default), \"allow_listed\" (requires ingress blocks), or \"deny_all\".",
				Validators:  []validator.String{stringvalidator.OneOf("allow_all", "allow_listed", "deny_all")},
			},
			"egress_mode": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: "\"allow_all\" (default), \"allow_listed\" (requires egress blocks), or \"deny_all\".",
				Validators:  []validator.String{stringvalidator.OneOf("allow_all", "allow_listed", "deny_all")},
			},
			"status":  schema.StringAttribute{Computed: true},
			"user_id": schema.StringAttribute{Computed: true},
			"vpc_id":  schema.StringAttribute{Computed: true},
		},
		Blocks: map[string]schema.Block{
			"ingress": schema.ListNestedBlock{NestedObject: sgRuleBlock()},
			"egress":  schema.ListNestedBlock{NestedObject: sgRuleBlock()},
		},
	}
}

func (r *sgResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *sgResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sgModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ingressMode, err := normalizeMode(plan.IngressMode)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("ingress_mode"), "Invalid ingress_mode", err.Error())
		return
	}
	egressMode, err := normalizeMode(plan.EgressMode)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("egress_mode"), "Invalid egress_mode", err.Error())
		return
	}

	ingress, err := buildRules(ingressMode, plan.Ingress)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ingress rules", err.Error())
		return
	}
	egress, err := buildRules(egressMode, plan.Egress)
	if err != nil {
		resp.Diagnostics.AddError("Invalid egress rules", err.Error())
		return
	}

	out, err := r.c.CreateSecurityGroup(ctx, client.CreateSecurityGroupRequest{
		ClusterID:    plan.ClusterID.ValueString(),
		Name:         plan.Name.ValueString(),
		IngressRules: ingress,
		EgressRules:  egress,
	})
	if err != nil {
		resp.Diagnostics.AddError("Create security group failed", err.Error())
		return
	}

	id := out.ID
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetSecurityGroup(ctx, id)
		if err != nil {
			return err
		}
		out = got
		if isReady(got.Status) {
			return errStopPolling
		}
		if terminalFailure(got.Status) {
			return fmt.Errorf("security group %s entered terminal status %q", id, got.Status)
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for SG failed", err.Error())
		return
	}

	r.fill(&plan, out, ingressMode, egressMode)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sgResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sgModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetSecurityGroup(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read SG failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}
	ingressMode := apiToMode(out.IngressRules)
	egressMode := apiToMode(out.EgressRules)
	r.fill(&state, out, ingressMode, egressMode)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *sgResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sgModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ingressMode, err := normalizeMode(plan.IngressMode)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("ingress_mode"), "Invalid ingress_mode", err.Error())
		return
	}
	egressMode, err := normalizeMode(plan.EgressMode)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("egress_mode"), "Invalid egress_mode", err.Error())
		return
	}
	ingress, err := buildRules(ingressMode, plan.Ingress)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ingress rules", err.Error())
		return
	}
	egress, err := buildRules(egressMode, plan.Egress)
	if err != nil {
		resp.Diagnostics.AddError("Invalid egress rules", err.Error())
		return
	}

	upd := client.UpdateSecurityGroupRequest{}
	if !plan.Name.Equal(state.Name) {
		n := plan.Name.ValueString()
		upd.Name = &n
	}

	stateIngressMode := state.IngressMode.ValueString()
	if stateIngressMode == "" {
		stateIngressMode = "allow_all"
	}
	stateEgressMode := state.EgressMode.ValueString()
	if stateEgressMode == "" {
		stateEgressMode = "allow_all"
	}
	if ingressMode != stateIngressMode || !rulesEqual(plan.Ingress, state.Ingress) {
		upd.IngressRules = &ingress
	}
	if egressMode != stateEgressMode || !rulesEqual(plan.Egress, state.Egress) {
		upd.EgressRules = &egress
	}

	out, err := r.c.UpdateSecurityGroup(ctx, state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Update SG failed", err.Error())
		return
	}
	r.fill(&plan, out, ingressMode, egressMode)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sgResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sgModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.c.DeleteSecurityGroup(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete SG failed", err.Error())
		return
	}
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetSecurityGroup(ctx, id)
		if err != nil {
			if client.IsNotFound(err) {
				return errStopPolling
			}
			return err
		}
		if isRemoved(got.Status) {
			return errStopPolling
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for SG deletion failed", err.Error())
	}
}

func (r *sgResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *sgResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		&modeBlocksValidator{direction: "ingress"},
		&modeBlocksValidator{direction: "egress"},
	}
}

type modeBlocksValidator struct{ direction string }

func (v *modeBlocksValidator) Description(_ context.Context) string {
	return "ensures " + v.direction + "_mode aligns with the presence of " + v.direction + " blocks"
}
func (v *modeBlocksValidator) MarkdownDescription(_ context.Context) string {
	return v.Description(context.Background())
}
func (v *modeBlocksValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m sgModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	mode := "allow_all"
	blocks := m.Ingress
	if v.direction == "egress" {
		blocks = m.Egress
		if !m.EgressMode.IsNull() && !m.EgressMode.IsUnknown() {
			mode = m.EgressMode.ValueString()
		}
	} else if !m.IngressMode.IsNull() && !m.IngressMode.IsUnknown() {
		mode = m.IngressMode.ValueString()
	}
	switch mode {
	case "allow_all", "deny_all":
		if len(blocks) > 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root(v.direction),
				"Blocks not allowed",
				fmt.Sprintf("%s_mode = %q does not allow %s blocks", v.direction, mode, v.direction),
			)
		}
	case "allow_listed":
		if len(blocks) == 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root(v.direction),
				"Missing rule blocks",
				fmt.Sprintf("%s_mode = \"allow_listed\" requires at least one %s block", v.direction, v.direction),
			)
		}
	}
}

func (r *sgResource) fill(m *sgModel, sg *client.SecurityGroup, ingressMode, egressMode string) {
	m.ID = types.StringValue(sg.ID)
	m.ClusterID = types.StringValue(sg.ClusterID)
	m.Name = types.StringValue(sg.Name)
	m.IngressMode = types.StringValue(ingressMode)
	m.EgressMode = types.StringValue(egressMode)
	m.Status = types.StringValue(sg.Status)
	m.UserID = types.StringValue(sg.UserID)
	m.VPCID = types.StringValue(sg.VPCID)
	if ingressMode == "allow_listed" {
		m.Ingress = rulesToModel(sg.IngressRules.Rules)
	} else {
		m.Ingress = nil
	}
	if egressMode == "allow_listed" {
		m.Egress = rulesToModel(sg.EgressRules.Rules)
	} else {
		m.Egress = nil
	}
}
