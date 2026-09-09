package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
// Read. boot_disk is its own block; network_interface blocks describe the
// VM's interfaces (see vm_network.go). subnet_ids, network_interface_ids and
// public_ip_id are computed mirrors of what the API reports.
type vmModel struct {
	ID              types.String `tfsdk:"id"`
	ClusterID       types.String `tfsdk:"cluster_id"`
	Name            types.String `tfsdk:"name"`
	ConfigurationID types.String `tfsdk:"configuration_id"`

	BootDisk    *vmBootDiskModel `tfsdk:"boot_disk"`
	NICs        []vmNICModel     `tfsdk:"network_interface"`
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
// storage by storage_id, OR supply volume_gb + image_id (+ name) to have the
// draft create one from the image catalog. The block is ForceNew on any
// change because a boot disk is frozen once the VM is provisioned.
type vmBootDiskModel struct {
	StorageID types.String `tfsdk:"storage_id"`
	Name      types.String `tfsdk:"name"`
	VolumeGb  types.Int64  `tfsdk:"volume_gb"`
	ImageID   types.String `tfsdk:"image_id"`
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
				Description:   "ID of the public IP attached to the VM, if any (mirror of the public network_interface block).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"restart_required": schema.BoolAttribute{Computed: true},
			"created_at":       schema.StringAttribute{Computed: true},
			"updated_at":       schema.StringAttribute{Computed: true},
		},
		Blocks: map[string]schema.Block{
			"network_interface": networkInterfaceBlock(),
			"boot_disk": schema.SingleNestedBlock{
				Description: "Boot disk: either reference an existing storage_id, or supply volume_gb + image_id (+ name) to create one from the image catalog while the VM is a draft. The boot disk cannot be changed in place; any modification forces a new VM.",
				Attributes: map[string]schema.Attribute{
					"storage_id": schema.StringAttribute{
						Optional:      true,
						Description:   "Existing storage ID to use as the boot disk. Mutually exclusive with the inline create fields.",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
						Validators:    []validator.String{validators.UUID()},
					},
					"name": schema.StringAttribute{
						Optional:      true,
						Description:   "Name of the inline-created boot disk. Defaults to a server-chosen name.",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"volume_gb": schema.Int64Attribute{
						Optional:      true,
						Description:   "Size of the inline-created boot disk in GB.",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
					},
					"image_id": schema.StringAttribute{
						Optional:      true,
						Description:   "Catalog image id for the inline-created boot disk (a 32-hex id, not a UUID). See the cloudless_default_images data source.",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
						Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
					},
				},
			},
		},
	}
}

func (r *vmResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// discardTimeout bounds the best-effort draft discard after a failed create.
const discardTimeout = 30 * time.Second

// bootDiskToAPI translates the Terraform boot_disk block into the /v3
// boot-disk body: an existing storage ID, or an inline create from a catalog
// image.
func bootDiskToAPI(d *vmBootDiskModel) (client.DraftBootDisk, error) {
	if d == nil {
		return client.DraftBootDisk{}, errors.New("boot_disk block is required")
	}
	if !d.StorageID.IsNull() && !d.StorageID.IsUnknown() && d.StorageID.ValueString() != "" {
		s := d.StorageID.ValueString()
		return client.DraftBootDisk{StorageID: &s}, nil
	}
	if d.VolumeGb.IsNull() || d.ImageID.IsNull() {
		return client.DraftBootDisk{}, errors.New("inline boot_disk requires volume_gb and image_id")
	}
	create := &client.CreateDraftBootDisk{
		VolumeGb: uint32(d.VolumeGb.ValueInt64()),
		ImageID:  d.ImageID.ValueString(),
	}
	if !d.Name.IsNull() && d.Name.ValueString() != "" {
		n := d.Name.ValueString()
		create.Name = &n
	}
	return client.DraftBootDisk{Create: create}, nil
}

// (stringsFromList and listFromStrings live in util.go for use by other resources.)

func (r *vmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bd, err := bootDiskToAPI(plan.BootDisk)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("boot_disk"), "Invalid boot_disk", err.Error())
		return
	}
	if _, verr := validateNICLayout(plan.NICs, false); verr != nil {
		resp.Diagnostics.Append(nicDiagnostic(verr))
		return
	}

	// Nothing declared, nothing managed: only a configuration with blocks
	// gets blocks in state; surfacing interfaces is import-time behaviour.
	noBlocks := len(plan.NICs) == 0
	keepBlocks := func() {
		if noBlocks {
			plan.NICs = nil
		}
	}

	id, err := r.createDraft(ctx, &plan, bd)
	if err != nil {
		resp.Diagnostics.AddError("Create VM failed", err.Error())
		return
	}

	out, err := pollUntilReady(ctx,
		func(ctx context.Context) (*client.VM, error) { return r.c.GetVM(ctx, id) },
		func(v *client.VM) string { return v.Status },
		"vm "+id,
	)
	if err != nil {
		resp.Diagnostics.AddError("Waiting for VM failed", err.Error())
		// Provision was accepted, so the VM exists and bills: record whatever
		// the API reports so destroy can clean it up instead of orphaning it.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discardTimeout)
		defer cancel()
		if out == nil {
			out, _ = r.c.GetVM(sctx, id)
		}
		if out != nil {
			resp.Diagnostics.Append(r.fillWithNICs(sctx, &plan, out)...)
			keepBlocks()
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		}
		return
	}

	// Existing public IPs attach to the live VM only (the API forbids it on a
	// draft); this is the one step of Create that may restart the VM.
	if aerr := attachExistingPublicNICs(ctx, r.c, id, plan.NICs); aerr != nil {
		resp.Diagnostics.AddError("Attach public IPs failed", aerr.Error())
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discardTimeout)
		defer cancel()
		resp.Diagnostics.Append(r.fillWithNICs(sctx, &plan, out)...)
		keepBlocks()
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	if out, err = r.c.GetVM(ctx, id); err != nil {
		resp.Diagnostics.AddError("Read VM after create failed", err.Error())
		return
	}

	resp.Diagnostics.Append(r.fillWithNICs(ctx, &plan, out)...)
	keepBlocks()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// createDraft runs the /v3 draft flow: create a draft with server defaults,
// refine each aspect the plan sets, then provision. A draft is free and
// unallocated, so on any failure before provision it is discarded rather than
// left for the user to find. Returns the VM ID once provision was accepted
// (graph @cloudless/fluence, node #1790).
func (r *vmResource) createDraft(ctx context.Context, plan *vmModel, bd client.DraftBootDisk) (string, error) {
	draft, err := r.c.CreateVMDraft(ctx)
	if err != nil {
		return "", fmt.Errorf("create draft: %w", err)
	}
	id := draft.ID

	// Public IPs the draft created for itself; released on discard because
	// the draft delete's cascade is unobserved (graph @cloudless/fluence,
	// node #1814) and a leaked address bills.
	var ownedIPs []string
	discard := func(step string, err error) error { return r.discardDraft(ctx, id, ownedIPs, step, err) }

	if want := plan.ClusterID.ValueString(); want != draft.ClusterID {
		if _, merr := r.c.MoveDraftVMToCluster(ctx, id, want, draft.UpdatedAt); merr != nil {
			return "", discard("move draft to cluster", merr)
		}
	}

	name := plan.Name.ValueString()
	cfg := plan.ConfigurationID.ValueString()
	if _, uerr := r.c.UpdateDraftVM(ctx, id, client.UpdateDraftVMRequest{Name: &name, ConfigurationID: &cfg}); uerr != nil {
		return "", discard("set draft name/configuration", uerr)
	}

	if _, berr := r.c.ReplaceDraftBootDisk(ctx, id, bd); berr != nil {
		return "", discard("set draft boot disk", berr)
	}

	// The server seeds a draft with all of the user's SSH keys; only override
	// when the plan names a set, so an omitted ssh_key_ids keeps that default.
	if !plan.SSHKeyIDs.IsNull() {
		if _, kerr := r.c.ReplaceDraftSSHKeys(ctx, id, stringsFromList(plan.SSHKeyIDs)); kerr != nil {
			return "", discard("set draft ssh keys", kerr)
		}
	}

	for _, storageID := range stringsFromList(plan.DataDiskIDs) {
		if _, serr := r.c.AttachDraftDataDisk(ctx, id, storageID); serr != nil {
			return "", discard("attach data disk "+storageID, serr)
		}
	}

	var nerr error
	if ownedIPs, nerr = assembleDraftNICs(ctx, r.c, id, plan.NICs); nerr != nil {
		return "", discard("assemble network interfaces", nerr)
	}

	if _, perr := r.c.ProvisionVM(ctx, id); perr != nil && !r.provisionLanded(ctx, id, perr) {
		return "", discard("provision", perr)
	}
	return id, nil
}

// discardDraft deletes a draft that failed mid-assembly, plus the public IPs
// it created, and wraps the step's error. The failure may be the caller's
// context dying, so the discard runs on a fresh deadline.
func (r *vmResource) discardDraft(ctx context.Context, id string, ownedIPs []string, step string, err error) error {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discardTimeout)
	defer cancel()
	if derr := r.c.DeleteVMDraft(dctx, id); derr != nil && !client.IsNotFound(derr) {
		return fmt.Errorf("%s: %w (and discarding draft %s failed: %w)", step, err, id, derr)
	}
	for _, ipID := range ownedIPs {
		if ierr := r.c.DeletePublicIP(dctx, ipID); ierr != nil && !client.IsNotFound(ierr) {
			return fmt.Errorf("%s: %w (and releasing draft-created public IP %s failed: %w)", step, err, ipID, ierr)
		}
	}
	return fmt.Errorf("%s: %w", step, err)
}

// provisionLanded reports whether a provision call that failed in transit
// was nevertheless accepted: a provisioned VM is no longer a draft, cannot
// be discarded, and bills — so the caller must keep its id and poll it.
func (r *vmResource) provisionLanded(ctx context.Context, id string, perr error) bool {
	if !isTransient(perr) {
		return false
	}
	vm, gerr := r.c.GetVM(context.WithoutCancel(ctx), id)
	return gerr == nil && vm.Status != statusDraft
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

	resp.Diagnostics.Append(r.fillWithNICs(ctx, &state, out)...)
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

	// Each step returns an error to report; a partial failure still refreshes
	// state so the next plan sees what really happened.
	steps := []struct {
		what string
		run  func() error
	}{
		{"Update VM", func() error { return r.updateName(ctx, id, state, plan) }},
		{"Attach VM storages", func() error { return r.attachDataDisks(ctx, id, state, plan) }},
		{"Detach VM storages", func() error { return r.detachDataDisks(ctx, id, state, plan) }},
		{"Update VM network interfaces", func() error { return r.updateNICs(ctx, id, state, plan) }},
	}
	for _, st := range steps {
		if err := st.run(); err != nil {
			resp.Diagnostics.AddError(st.what+" failed", err.Error())
			r.refreshInto(ctx, id, &plan, resp)
			return
		}
	}

	got, err := r.c.GetVM(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Read VM after update failed", err.Error())
		return
	}
	resp.Diagnostics.Append(r.fillWithNICs(ctx, &plan, got)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// refreshInto records the VM's current state after a partial update.
func (r *vmResource) refreshInto(ctx context.Context, id string, plan *vmModel, resp *resource.UpdateResponse) {
	got, err := r.c.GetVM(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Read VM after partial update failed", err.Error())
		return
	}
	resp.Diagnostics.Append(r.fillWithNICs(ctx, plan, got)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *vmResource) updateName(ctx context.Context, id string, state, plan vmModel) error {
	if plan.Name.Equal(state.Name) {
		return nil
	}
	v := plan.Name.ValueString()
	_, err := r.c.UpdateVM(ctx, id, client.UpdateVMRequest{Name: &v})
	return err
}

// attachDataDisks and detachDataDisks diff data_disk_ids and call
// /storages/add and /storages/remove; the API has no "set" operation.
func (r *vmResource) attachDataDisks(ctx context.Context, id string, state, plan vmModel) error {
	toAdd, _ := diffStrings(stringsFromList(state.DataDiskIDs), stringsFromList(plan.DataDiskIDs))
	if len(toAdd) == 0 {
		return nil
	}
	return r.c.AddVMStorages(ctx, id, toAdd)
}

func (r *vmResource) detachDataDisks(ctx context.Context, id string, state, plan vmModel) error {
	_, toRemove := diffStrings(stringsFromList(state.DataDiskIDs), stringsFromList(plan.DataDiskIDs))
	if len(toRemove) == 0 {
		return nil
	}
	return r.c.RemoveVMStorages(ctx, id, toRemove)
}

// updateNICs reconciles network_interface blocks on the live VM and applies
// the restart the API asks for afterwards, so the configuration is in effect
// when apply returns.
func (r *vmResource) updateNICs(ctx context.Context, id string, state, plan vmModel) error {
	if len(plan.NICs) == 0 && len(state.NICs) == 0 {
		return nil
	}
	if _, err := validateNICLayout(plan.NICs, false); err != nil {
		return err
	}
	changed, err := reconcileLiveNICs(ctx, r.c, id, state.NICs, plan.NICs)
	if err != nil || !changed {
		return err
	}
	vm, err := r.c.GetVM(ctx, id)
	if err != nil {
		return err
	}
	if vm.RestartRequired {
		if rerr := restartAndWaitReady(ctx, r.c, id); rerr != nil {
			return fmt.Errorf("restart: %w", rerr)
		}
	}
	return nil
}

func (r *vmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	// A draft that never reached provision holds no cluster resources and is
	// discarded through /v3; a live VM is terminated through /v2.
	if state.Status.ValueString() == statusDraft {
		if err := r.c.DeleteVMDraft(ctx, id); err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Discard VM draft failed", err.Error())
		}
		// The draft discard is documented to cascade draft-created IPs, but
		// that is unobserved (graph @cloudless/fluence, node #1814); release
		// them the same way the live path does, tolerating "already gone".
		r.releaseOwnedIPs(ctx, state.NICs, &resp.Diagnostics)
		return
	}
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
		bootID := state.BootDiskID.ValueString()
		err := retryTransient(ctx, func(ctx context.Context) error { return r.c.DeleteStorage(ctx, bootID) })
		if err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Deleting boot disk failed", err.Error())
		}
	}

	r.releaseOwnedIPs(ctx, state.NICs, &resp.Diagnostics)
}

// releaseOwnedIPs deletes the public IPs the VM created for itself: they
// have no resource of their own, and terminate does not cascade them
// (observed on stage; graph @cloudless/fluence, node #1814).
func (r *vmResource) releaseOwnedIPs(ctx context.Context, nics []vmNICModel, diags *diag.Diagnostics) {
	for _, ipID := range ownedPublicIPs(nics) {
		err := retryTransient(ctx, func(ctx context.Context) error { return r.c.DeletePublicIP(ctx, ipID) })
		if err != nil && !client.IsNotFound(err) {
			diags.AddError("Releasing VM-owned public IP "+ipID+" failed", err.Error())
		}
	}
}

// ModifyPlan validates network_interface blocks at plan time and marks the
// changes a live VM cannot absorb as replacements.
func (r *vmResource) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan, cfg vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Ownership of a public IP is a configuration fact; stamp it into the plan
	// so Create/Update and the replacement rules can read it.
	plan.NICs = resolveNICOwnership(cfg.NICs, plan.NICs)
	for i := range plan.NICs {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx,
			path.Root("network_interface").AtListIndex(i).AtName("address_type"), plan.NICs[i].AddressType)...)
	}
	// Layout rules are about what the user wrote; `default` carried from
	// state is the API's word and is not re-judged here.
	if _, err := validateNICs(cfg.NICs); err != nil {
		resp.Diagnostics.Append(nicDiagnostic(err))
		return
	}
	if req.State.Raw.IsNull() {
		return
	}
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if replace, why := planNICReplacement(state.NICs, plan.NICs); replace {
		resp.Diagnostics.AddAttributeWarning(path.Root("network_interface"),
			"VM will be replaced", why+"; a live VM cannot change this in place.")
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("network_interface"))
		return
	}
	if nicKeysDiffer(state.NICs, plan.NICs) {
		// The VM-level mirrors change with the interfaces; UseStateForUnknown
		// would otherwise carry stale values into the plan.
		for _, name := range []string{"network_interface_ids", "subnet_ids"} {
			resp.Diagnostics.Append(
				resp.Plan.SetAttribute(ctx, path.Root(name), types.ListUnknown(types.StringType))...)
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("public_ip_id"), types.StringUnknown())...)
	}
}

// fillWithNICs fills the model from the VM and its interfaces.
func (r *vmResource) fillWithNICs(ctx context.Context, m *vmModel, v *client.VM) diag.Diagnostics {
	var diags diag.Diagnostics
	r.fill(m, v)
	ifaces, err := r.c.ListVMInterfaces(ctx, v.ID)
	if err != nil {
		diags.AddError("Read VM network interfaces failed", err.Error())
		return diags
	}
	m.NICs = nicsFromAPI(m.NICs, ifaces)
	return diags
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
	// The API lists these in arbitrary order (it changes across a restart);
	// sort so the computed mirrors are stable between plan and apply.
	m.Subnets = listFromStrings(sortedCopy(v.Subnets))
	m.NetworkInterfaces = listFromStrings(sortedCopy(v.NetworkInterfaces))
	m.PublicIPID = stringFromPtr(v.PublicIP)
	m.RestartRequired = types.BoolValue(v.RestartRequired)
	m.CreatedAt = types.StringValue(v.CreatedAt)
	m.UpdatedAt = types.StringValue(v.UpdatedAt)
}
