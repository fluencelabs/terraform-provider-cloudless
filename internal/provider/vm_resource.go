package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"
)

func NewVMResource() resource.Resource { return &vmResource{} }

type vmResource struct {
	c *client.Client
}

// vmModel: data_disk_ids is the user-controlled attach list, refreshed on
// Read. Boot disk is its own block (either an existing storage_id or inline
// create fields). The inline public_ip block is gone — public IP attachment
// lives in cloudless_vm_public_ip_attachment (Task 8). public_ip_id is
// retained as a Computed mirror so users can read whatever the API reports.
type vmModel struct {
	ID              types.String `tfsdk:"id"`
	ClusterID       types.String `tfsdk:"cluster_id"`
	Name            types.String `tfsdk:"name"`
	ConfigurationID types.String `tfsdk:"configuration_id"`

	BootDisk    *vmBootDiskModel `tfsdk:"boot_disk"`
	DataDiskIDs types.List       `tfsdk:"data_disk_ids"`
	SSHKeyIDs   types.List       `tfsdk:"ssh_key_ids"`

	Status            types.String `tfsdk:"status"`
	UserID            types.String `tfsdk:"user_id"`
	BootDiskID        types.String `tfsdk:"boot_disk_id"`
	Subnets           types.List   `tfsdk:"subnet_ids"`
	NetworkInterfaces types.List   `tfsdk:"network_interface_ids"`
	PublicIPID        types.String `tfsdk:"public_ip_id"`
	RestartRequired   types.Bool   `tfsdk:"restart_required"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

// vmBootDiskModel mirrors the boot_disk block: either reference an existing
// storage by storage_id, OR supply name + storage_type + volume_gb +
// replicated (+ os_image) to create one inline. The block is ForceNew on any
// change because the API does not support changing the boot disk in place.
type vmBootDiskModel struct {
	StorageID   types.String `tfsdk:"storage_id"`
	Name        types.String `tfsdk:"name"`
	StorageType types.String `tfsdk:"storage_type"`
	VolumeGb    types.Int64  `tfsdk:"volume_gb"`
	Replicated  types.Bool   `tfsdk:"replicated"`
	OSImage     types.String `tfsdk:"os_image"`
}

func (r *vmResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm"
}

func (r *vmResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A virtual machine on a Fluence cluster.",
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
			"configuration_id": schema.StringAttribute{
				Required:      true,
				Description:   "VM configuration (CPU/RAM preset) UUID. See the cloudless_vm_configurations data source.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UUID()},
			},
			"data_disk_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: "IDs of data storage volumes to attach. Add/remove via the smart Update path; not a force-replace.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{listvalidator.ValueStringsAre(validators.UUID())},
			},
			"ssh_key_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "SSH keys to install at first boot. Changing this forces a new VM — Fluence applies SSH keys at create time only.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
				Validators: []validator.List{listvalidator.ValueStringsAre(validators.UUID())},
			},
			"status":       schema.StringAttribute{Computed: true},
			"user_id":      schema.StringAttribute{Computed: true},
			"boot_disk_id": schema.StringAttribute{Computed: true},
			"subnet_ids": schema.ListAttribute{
				ElementType:   types.StringType,
				Computed:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"network_interface_ids": schema.ListAttribute{
				ElementType:   types.StringType,
				Computed:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"public_ip_id": schema.StringAttribute{
				Computed:      true,
				Description:   "ID of the attached public IP, if any. Manage attachment via cloudless_vm_public_ip_attachment.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"restart_required": schema.BoolAttribute{Computed: true},
			"created_at":       schema.StringAttribute{Computed: true},
			"updated_at":       schema.StringAttribute{Computed: true},
		},
		Blocks: map[string]schema.Block{
			"boot_disk": schema.SingleNestedBlock{
				Description: "Boot disk: either reference an existing storage_id or supply name + storage_type + volume_gb + replicated + os_image to create one inline. The boot disk cannot be changed in place; any modification forces a new VM.",
				Attributes: map[string]schema.Attribute{
					"storage_id": schema.StringAttribute{
						Optional:      true,
						Description:   "Existing storage ID to attach. Mutually exclusive with the inline create fields.",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
						Validators:    []validator.String{validators.UUID()},
					},
					"name": schema.StringAttribute{
						Optional:      true,
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"storage_type": schema.StringAttribute{
						Optional:      true,
						Validators:    []validator.String{stringvalidator.OneOf("NVME")},
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"volume_gb": schema.Int64Attribute{
						Optional:      true,
						PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
					},
					"replicated": schema.BoolAttribute{
						Optional:      true,
						PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
					},
					"os_image": schema.StringAttribute{
						Optional:      true,
						Description:   "URL of an OS image (only valid for inline-create boot disk).",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
				},
			},
		},
	}
}

func (r *vmResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// bootDiskToAPI translates the Terraform boot_disk block into a VMBootDisk for
// the Fluence API: existing storage_id wins; otherwise the inline-create
// fields are required as a set. The inline-create variant is a
// CreateUserStorageRequest, which carries its own clusterId — it inherits the
// VM's cluster, passed in here.
func bootDiskToAPI(d *vmBootDiskModel, clusterID string) (client.VMBootDisk, error) {
	if d == nil {
		return client.VMBootDisk{}, errors.New("boot_disk block is required")
	}
	if !d.StorageID.IsNull() && !d.StorageID.IsUnknown() && d.StorageID.ValueString() != "" {
		s := d.StorageID.ValueString()
		return client.VMBootDisk{StorageID: &s}, nil
	}
	if d.Name.IsNull() || d.StorageType.IsNull() || d.VolumeGb.IsNull() || d.Replicated.IsNull() {
		return client.VMBootDisk{}, errors.New(
			"inline boot_disk requires name, storage_type, volume_gb, and replicated",
		)
	}
	return client.VMBootDisk{Create: &client.CreateUserStorageInline{
		ClusterID:   clusterID,
		Name:        d.Name.ValueString(),
		StorageType: d.StorageType.ValueString(),
		VolumeGb:    uint32(d.VolumeGb.ValueInt64()),
		Replicated:  d.Replicated.ValueBool(),
		OSImage:     d.OSImage.ValueString(),
	}}, nil
}

// (stringsFromList and listFromStrings live in util.go for use by other resources.)

func (r *vmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bd, err := bootDiskToAPI(plan.BootDisk, plan.ClusterID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("boot_disk"), "Invalid boot_disk", err.Error())
		return
	}

	dataDisks := stringsFromList(plan.DataDiskIDs)
	sshKeys := stringsFromList(plan.SSHKeyIDs)

	out, err := r.c.CreateVM(ctx, client.CreateVMRequest{
		ClusterID:       plan.ClusterID.ValueString(),
		Name:            plan.Name.ValueString(),
		ConfigurationID: plan.ConfigurationID.ValueString(),
		BootDisk:        bd,
		DataDisks:       dataDisks,
		SSHKeys:         sshKeys,
	})
	if err != nil {
		resp.Diagnostics.AddError("Create VM failed", err.Error())
		return
	}

	id := out.ID
	out, err = pollUntilReady(ctx,
		func(ctx context.Context) (*client.VM, error) { return r.c.GetVM(ctx, id) },
		func(v *client.VM) string { return v.Status },
		"vm "+id,
	)
	if err != nil {
		resp.Diagnostics.AddError("Waiting for VM failed", err.Error())
		return
	}

	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.GetVM(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read VM failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}

	r.fill(&state, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		if _, err := r.c.UpdateVM(ctx, id, client.UpdateVMRequest{Name: &v}); err != nil {
			resp.Diagnostics.AddError("Update VM failed", err.Error())
			return
		}
	}

	// data_disk_ids smart Update: diff old vs new and call /storages/add and
	// /storages/remove. The API does not accept a "set" operation.
	toAdd, toRemove := diffStrings(stringsFromList(state.DataDiskIDs), stringsFromList(plan.DataDiskIDs))
	refreshAndSet := func() {
		got, err := r.c.GetVM(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Read VM after partial update failed", err.Error())
			return
		}
		r.fill(&plan, got)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}

	if len(toAdd) > 0 {
		if err := r.c.AddVMStorages(ctx, id, toAdd); err != nil {
			resp.Diagnostics.AddError("Attach VM storages failed", err.Error())
			refreshAndSet()
			return
		}
	}
	if len(toRemove) > 0 {
		if err := r.c.RemoveVMStorages(ctx, id, toRemove); err != nil {
			resp.Diagnostics.AddError("Detach VM storages failed", err.Error())
			refreshAndSet()
			return
		}
	}

	got, err := r.c.GetVM(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Read VM after update failed", err.Error())
		return
	}
	r.fill(&plan, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.c.TerminateVM(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Terminate VM failed", err.Error())
		return
	}
	if err := pollUntilGone(ctx,
		func(ctx context.Context) (*client.VM, error) { return r.c.GetVM(ctx, id) },
		func(v *client.VM) string { return v.Status },
		"vm "+id,
	); err != nil {
		resp.Diagnostics.AddError("Waiting for VM termination failed", err.Error())
		return
	}

	// An inline-create boot disk is owned by this VM (it is not a separate
	// cloudless_storage resource), and VM terminate does not cascade it, so it
	// would otherwise be orphaned. Delete it now that the VM is gone. Skip when
	// boot_disk references an existing storage_id — that volume is managed
	// elsewhere and must not be deleted here.
	if bd := state.BootDisk; bd != nil &&
		(bd.StorageID.IsNull() || bd.StorageID.ValueString() == "") &&
		!state.BootDiskID.IsNull() && state.BootDiskID.ValueString() != "" {
		if err := r.c.DeleteStorage(ctx, state.BootDiskID.ValueString()); err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Deleting boot disk failed", err.Error())
		}
	}
}

func (r *vmResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// fill copies API data into the model. Computed list fields use types.List
// (not []types.String) so unknown values plan correctly.
func (r *vmResource) fill(m *vmModel, v *client.VM) {
	m.ID = types.StringValue(v.ID)
	m.ClusterID = types.StringValue(v.ClusterID)
	m.ConfigurationID = types.StringValue(v.ConfigurationID)
	m.Name = types.StringValue(v.Name)
	m.Status = types.StringValue(v.Status)
	m.UserID = types.StringValue(v.UserID)
	m.BootDiskID = stringFromPtr(v.BootDisk)
	m.DataDiskIDs = listFromStrings(v.DataDisks)
	m.Subnets = listFromStrings(v.Subnets)
	m.NetworkInterfaces = listFromStrings(v.NetworkInterfaces)
	m.PublicIPID = stringFromPtr(v.PublicIP)
	m.RestartRequired = types.BoolValue(v.RestartRequired)
	m.CreatedAt = types.StringValue(v.CreatedAt)
	m.UpdatedAt = types.StringValue(v.UpdatedAt)
}
