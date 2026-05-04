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

func NewVMPublicIPAttachmentResource() resource.Resource { return &vmPublicIPAttachmentResource{} }

type vmPublicIPAttachmentResource struct{ c *client.Client }

type vmPublicIPAttachmentModel struct {
	ID         types.String `tfsdk:"id"`
	VMID       types.String `tfsdk:"vm_id"`
	PublicIPID types.String `tfsdk:"public_ip_id"`
}

func (r *vmPublicIPAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_public_ip_attachment"
}

func (r *vmPublicIPAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Attach a cloudless_public_ip to a cloudless_vm. Both attributes are ForceNew.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"vm_id":        schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Validators: []validator.String{validators.UUID()}},
			"public_ip_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Validators: []validator.String{validators.UUID()}},
		},
	}
}

func (r *vmPublicIPAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *vmPublicIPAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmPublicIPAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.AddVMPublicIP(ctx, plan.VMID.ValueString(), plan.PublicIPID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Attach public IP failed", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.VMID.ValueString() + ":" + plan.PublicIPID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmPublicIPAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmPublicIPAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err := r.c.GetVM(ctx, state.VMID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read VM failed", err.Error())
		return
	}
	if vm.PublicIP == nil || *vm.PublicIP != state.PublicIPID.ValueString() {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmPublicIPAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Both attributes are RequiresReplace, so Update should never be called.
	// If it is (e.g., a future schema field is added), preserve state.
	var plan vmPublicIPAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmPublicIPAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmPublicIPAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// API: /public_ip/remove takes no body and removes whatever IP is bound.
	// Fetch current state first so we don't accidentally remove a different IP
	// the user attached out-of-band.
	vm, err := r.c.GetVM(ctx, state.VMID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Read VM during attachment delete failed", err.Error())
		return
	}
	if vm.PublicIP == nil || *vm.PublicIP != state.PublicIPID.ValueString() {
		// Already detached or replaced — nothing to do.
		return
	}
	if err := r.c.RemoveVMPublicIP(ctx, state.VMID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Detach public IP failed", err.Error())
	}
}

func (r *vmPublicIPAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, ":", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID", "expected <vm_id>:<public_ip_id>")
		return
	}
	resp.State.SetAttribute(ctx, path.Root("id"), req.ID)
	resp.State.SetAttribute(ctx, path.Root("vm_id"), parts[0])
	resp.State.SetAttribute(ctx, path.Root("public_ip_id"), parts[1])
}
