package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"
)

func NewVMRestartResource() resource.Resource { return &vmRestartResource{} }

type vmRestartResource struct{ c *client.Client }

type vmRestartModel struct {
	ID        types.String `tfsdk:"id"`
	VMID      types.String `tfsdk:"vm_id"`
	Triggers  types.Map    `tfsdk:"triggers"`
	Restarted types.Bool   `tfsdk:"restarted"`
}

func (r *vmRestartResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_vm_restart"
}

func (r *vmRestartResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Restart a VM once, but only if it is flagged restart_required. Attaching a public IP " +
			"(cloudless_vm_public_ip_attachment) or binding a security group (cloudless_security_group_attachment) " +
			"flags the VM restart_required, and the change only takes effect after a restart. Place this resource " +
			"after those attachments (via depends_on, or the triggers map) to apply all pending changes with a " +
			"single restart. It is a no-op when restart_required is not set, so it never reboots a healthy VM.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"vm_id": schema.StringAttribute{
				Required:      true,
				Description:   "VM to restart when restart_required is set.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UUID()},
			},
			"triggers": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Arbitrary values that force the restart to be re-evaluated when they change. " +
					"Wire attachment IDs here (e.g. the public-IP attachment id) so a new attach re-checks " +
					"restart_required. Changing any value forces this resource to be replaced.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"restarted": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether a restart was actually issued (false when the VM was not restart_required).",
			},
		},
	}
}

func (r *vmRestartResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *vmRestartResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmRestartModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	restarted, err := restartIfFlagged(ctx, r.c, plan.VMID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("VM restart failed", err.Error())
		return
	}
	plan.ID = plan.VMID
	plan.Restarted = types.BoolValue(restarted)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmRestartResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Nothing to refresh: a restart is a one-shot action, not a persistent
	// object. Keep state as-is; re-evaluation happens when triggers change
	// (which forces replacement).
	var state vmRestartModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmRestartResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// vm_id and triggers are RequiresReplace, so Update is only reached when
	// nothing meaningful changed. Preserve the prior result.
	var plan, state vmRestartModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	plan.Restarted = state.Restarted
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmRestartResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
	// Nothing to undo: the VM keeps running. Removing this resource just stops
	// managing the restart.
}

func (r *vmRestartResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vm_id"), req.ID)...)
}

// restartIfFlagged restarts the VM only when the API reports restart_required,
// and reports whether a restart was issued. Pending network changes (a public-IP
// attach, a security-group bind) set restart_required and do not take effect
// until the VM restarts.
func restartIfFlagged(ctx context.Context, c *client.Client, vmID string) (bool, error) {
	vm, err := c.GetVM(ctx, vmID)
	if err != nil {
		return false, err
	}
	if !vm.RestartRequired {
		return false, nil
	}
	if err = restartAndWaitReady(ctx, c, vmID); err != nil {
		return false, err
	}
	return true, nil
}

// restartAndWaitReady restarts the VM and blocks until it leaves the transitional
// restarting state and reports ready again. Right after an attach/bind the VM is
// briefly in a transitional state and the restart endpoint returns 406 ("VM is
// not in a status to ..."); retry until the VM settles and accepts the restart.
func restartAndWaitReady(ctx context.Context, c *client.Client, vmID string) error {
	err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		_, rErr := c.RestartVM(ctx, vmID)
		switch {
		case rErr == nil:
			return errStopPolling
		case client.IsNotAcceptable(rErr):
			return nil // not ready to restart yet; keep waiting
		default:
			return rErr
		}
	})
	if err != nil {
		return err
	}
	_, err = pollUntilReady(ctx,
		func(ctx context.Context) (*client.VM, error) { return c.GetVM(ctx, vmID) },
		func(v *client.VM) string { return v.Status },
		"vm "+vmID,
	)
	return err
}
