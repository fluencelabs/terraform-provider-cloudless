package provider

import (
	"context"
	"strings"

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

func NewSecurityGroupAttachmentResource() resource.Resource { return &sgAttachmentResource{} }

type sgAttachmentResource struct{ c *client.Client }

type sgAttachmentModel struct {
	ID                 types.String `tfsdk:"id"`
	NetworkInterfaceID types.String `tfsdk:"network_interface_id"`
	SecurityGroupID    types.String `tfsdk:"security_group_id"`
	VMID               types.String `tfsdk:"vm_id"`
}

func (r *sgAttachmentResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_security_group_attachment"
}

func (r *sgAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Bind a security group to a network interface (one SG per interface). VM ID is resolved at " +
			"create-time and stored in state. Binding flags the VM restart_required; add a cloudless_vm_restart " +
			"(depends_on this resource) to apply the rules with a single restart after all attachments.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"network_interface_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UUID()},
			},
			"security_group_id": schema.StringAttribute{
				Required: true,
				// In-place updatable: changes call PATCH with the new ID.
				Validators: []validator.String{validators.UUID()},
			},
			"vm_id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sgAttachmentResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *sgAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sgAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err := r.c.FindVMByInterface(ctx, plan.NetworkInterfaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Find VM for interface failed", err.Error())
		return
	}
	sgID := plan.SecurityGroupID.ValueString()
	if err = r.c.UpdateVMInterface(ctx, vm.ID, plan.NetworkInterfaceID.ValueString(), &sgID); err != nil {
		resp.Diagnostics.AddError("Bind SG failed", err.Error())
		return
	}
	plan.VMID = types.StringValue(vm.ID)
	plan.ID = plan.NetworkInterfaceID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sgAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sgAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ifaces, err := r.c.ListVMInterfaces(ctx, state.VMID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("List interfaces failed", err.Error())
		return
	}
	for _, ni := range ifaces {
		if ni.ID != state.NetworkInterfaceID.ValueString() {
			continue
		}
		if ni.SecurityGroupID == nil || *ni.SecurityGroupID != state.SecurityGroupID.ValueString() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	// Interface itself disappeared.
	resp.State.RemoveResource(ctx)
}

func (r *sgAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sgAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.SecurityGroupID.Equal(state.SecurityGroupID) {
		sgID := plan.SecurityGroupID.ValueString()
		if err := r.c.UpdateVMInterface(ctx, state.VMID.ValueString(), state.NetworkInterfaceID.ValueString(), &sgID); err != nil {
			resp.Diagnostics.AddError("Update SG binding failed", err.Error())
			return
		}
	}
	// vm_id and network_interface_id are immutable.
	plan.VMID = state.VMID
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sgAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sgAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.UpdateVMInterface(ctx, state.VMID.ValueString(), state.NetworkInterfaceID.ValueString(), nil); err != nil &&
		!client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unbind SG failed", err.Error())
	}
}

func (r *sgAttachmentResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	// Accept either "<vm_id>:<network_interface_id>" or "<network_interface_id>".
	parts := strings.SplitN(req.ID, ":", importIDParts)
	if len(parts) == importIDParts {
		if parts[0] == "" || parts[1] == "" {
			resp.Diagnostics.AddError("Invalid import ID", "expected <vm_id>:<network_interface_id>")
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_interface_id"), parts[1])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vm_id"), parts[0])...)
		return
	}
	// Single-id form: resolve vm_id by scanning.
	ifaceID := req.ID
	if ifaceID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"expected <network_interface_id> or <vm_id>:<network_interface_id>",
		)
		return
	}
	vm, err := r.c.FindVMByInterface(ctx, ifaceID)
	if err != nil {
		resp.Diagnostics.AddError("Resolve vm_id from interface failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ifaceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_interface_id"), ifaceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vm_id"), vm.ID)...)
	// security_group_id will be filled by Read on the next refresh.
}
