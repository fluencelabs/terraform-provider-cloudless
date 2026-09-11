package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"
)

// network_interface blocks of cloudless_vm. A VM's network is assembled on the
// draft and provisioned in one apply; on a live VM only add/remove and the
// security-group binding change in place. (graph @cloudless/fluence, nodes
// #1809, #1811, #1812)

// vmNICModel mirrors one network_interface block.
type vmNICModel struct {
	ID              types.String `tfsdk:"id"`
	Type            types.String `tfsdk:"type"`
	SubnetID        types.String `tfsdk:"subnet_id"`
	PublicIPID      types.String `tfsdk:"public_ip_id"`
	AddressType     types.String `tfsdk:"address_type"`
	SecurityGroupID types.String `tfsdk:"security_group_id"`
	StaticIPs       types.List   `tfsdk:"static_ips"`
	Default         types.Bool   `tfsdk:"default"`
	AssignedIPs     types.List   `tfsdk:"assigned_ips"`
}

const (
	nicTypePrivate = "private"
	nicTypePublic  = "public"

	defaultPublicIPAddressType = "V4"
)

// maxStaticIPsPerNIC: at most one static IP per IP version.
const maxStaticIPsPerNIC = 2

func networkInterfaceBlock() schema.ListNestedBlock {
	return schema.ListNestedBlock{
		Description: "Network interfaces of the VM. type = \"private\" binds a subnet (subnet_id); type = \"public\" attaches " +
			"an existing cloudless_public_ip (public_ip_id) or, when public_ip_id is omitted, makes the VM create and own a " +
			"public IP of address_type that is released on destroy. Exactly one private interface is the default, and it may " +
			"leave subnet_id out to stay on the cluster's default subnet — so a VM needs no VPC or subnet of its own. Omit " +
			"the blocks entirely and the network is left to the API. Interfaces are assembled before the VM is provisioned, " +
			"so no restart is needed. On a live VM interfaces can be added and removed, their security group changed, and " +
			"the default interface repointed to another subnet of the same cluster (a restart, not a new VM); changing " +
			"static_ips or a VM-owned public IP forces a new VM.",
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"id": schema.StringAttribute{Computed: true},
				"type": schema.StringAttribute{
					Required:    true,
					Description: "\"private\" (subnet) or \"public\" (public IP).",
					Validators:  []validator.String{stringvalidator.OneOf(nicTypePrivate, nicTypePublic)},
				},
				"subnet_id": schema.StringAttribute{
					Optional: true, Computed: true,
					PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
					Description: "Subnet of a private interface. The default interface may omit it — the VM then keeps " +
						"the cluster's default subnet and its id is computed here; every other private interface names one.",
					Validators: []validator.String{validators.UUID()},
				},
				"public_ip_id": schema.StringAttribute{
					Optional: true, Computed: true,
					Description: "Existing public IP to attach (type = \"public\"). Omit to let the VM create its own; the created IP's id is then computed here.",
					Validators:  []validator.String{validators.UUID()},
				},
				"address_type": schema.StringAttribute{
					Optional: true, Computed: true,
					Description: "Address type of a VM-created public IP (default V4). Null when public_ip_id attaches an existing IP.",
					Validators:  []validator.String{stringvalidator.OneOf(defaultPublicIPAddressType)},
				},
				"security_group_id": schema.StringAttribute{
					Optional:    true,
					Description: "Security group bound to this interface; must belong to the VM's VPC. Removing it unbinds the group.",
					Validators:  []validator.String{validators.UUID()},
				},
				"static_ips": schema.ListAttribute{
					ElementType: types.StringType,
					Optional:    true,
					Computed:    true,
					Description: "Static private IPs for a private interface (at most one per IP version, inside the subnet CIDR). Set only while the VM is created.",
					Validators:  []validator.List{listvalidator.SizeAtMost(maxStaticIPsPerNIC)},
				},
				"default": schema.BoolAttribute{
					Optional: true, Computed: true,
					Description: "Marks the VM's default interface. Must be a private interface; when unset, the first private interface is the default.",
				},
				"assigned_ips": schema.ListAttribute{
					ElementType: types.StringType,
					Computed:    true,
					Description: "Addresses observed on the live VM for this interface.",
				},
			},
		},
	}
}

// nicIsPublic reports whether the block describes a public interface.
func nicIsPublic(n vmNICModel) bool { return n.Type.ValueString() == nicTypePublic }

func knownString(v types.String) bool { return !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" }

// nicOwnsIP reports whether the VM creates (or created) this block's public
// IP itself. ModifyPlan decides it from the configuration (no public_ip_id
// written) and records it as a known address_type; an existing IP attached
// via public_ip_id keeps address_type null. Plan values alone cannot tell the
// two apart: a reference to a not-yet-created cloudless_public_ip is unknown
// exactly like an omitted one.
func nicOwnsIP(n vmNICModel) bool {
	return nicIsPublic(n) && knownString(n.AddressType)
}

// resolveNICOwnership stamps address_type from the configuration: "V4" (or
// the configured type) for a public block without public_ip_id, null
// otherwise. Returns the updated blocks.
func resolveNICOwnership(cfg, plan []vmNICModel) []vmNICModel {
	out := make([]vmNICModel, len(plan))
	copy(out, plan)
	for i := range out {
		if i >= len(cfg) || !nicIsPublic(out[i]) || !cfg[i].PublicIPID.IsNull() {
			out[i].AddressType = types.StringNull()
			continue
		}
		if knownString(cfg[i].AddressType) {
			out[i].AddressType = cfg[i].AddressType
		} else {
			out[i].AddressType = types.StringValue(defaultPublicIPAddressType)
		}
	}
	return out
}

// nicAttachesExistingIP reports whether the block attaches a user-managed
// public IP.
func nicAttachesExistingIP(n vmNICModel) bool {
	return nicIsPublic(n) && !nicOwnsIP(n)
}

// nicKey identifies an interface by what it is bound to, not by position:
// "prv:<subnet>" or "pub:<ip>". Owned public IPs whose ID is not yet known
// key as "pub:new" (graph @cloudless/fluence, node #1815).
func nicKey(n vmNICModel) string {
	if nicIsPublic(n) {
		if knownString(n.PublicIPID) {
			return "pub:" + n.PublicIPID.ValueString()
		}
		return "pub:new"
	}
	if !knownString(n.SubnetID) {
		// The default interface with no subnet named: whatever subnet the API
		// gave it is the one it is on.
		return "prv:default"
	}
	return "prv:" + n.SubnetID.ValueString()
}

func apiNICKey(i client.VMInterface) string {
	if i.IsPublic() {
		return "pub:" + i.PublicIPID()
	}
	return "prv:" + i.SubnetID()
}

// validateNICs checks a configured block list before any API call: exactly one
// private default, each block either private or public, no default on a
// public interface. Returns the index of the default block
// (graph @cloudless/fluence, node #1816).
func validateNICs(nics []vmNICModel) (int, error) {
	return validateNICLayout(nics, true)
}

// validateNICLayout is validateNICs with the default-interface rules made
// optional: `default` is also computed, and the API may flag a public
// interface as the VM's default on its own (seen on stage after attaching a
// reserved IP), so values carried from state are facts, not user errors.
// Configuration is checked with checkDefault=true, plan/state with false.
func validateNICLayout(nics []vmNICModel, checkDefault bool) (int, error) {
	if len(nics) == 0 {
		return -1, nil
	}
	dflt, firstPrivate := -1, -1
	subnets := map[string]int{}
	public := 0
	for i, n := range nics {
		private, err := validateNIC(i, n)
		if err != nil {
			return -1, err
		}
		if private && firstPrivate < 0 {
			firstPrivate = i
		}
		if !private {
			public++
		}
		if serr := noteSubnet(subnets, i, n, private); serr != nil {
			return -1, serr
		}
		if checkDefault {
			if dflt, err = noteDefault(dflt, i, n, private); err != nil {
				return -1, err
			}
		}
	}
	if public > 1 {
		return -1, errors.New("network_interface: a VM can have only one public interface")
	}
	if err := noteUnboundPrivate(nics); err != nil {
		return -1, err
	}
	if dflt < 0 {
		dflt = firstPrivate
	}
	if dflt < 0 {
		return -1, errors.New(
			"network_interface: at least one private interface (subnet_id) is required and becomes the default",
		)
	}
	return dflt, nil
}

// noteSubnet records which block binds a subnet and rejects a second one:
// interfaces are keyed by their subnet, so two private blocks on one subnet
// would be indistinguishable on the wire.
func noteSubnet(subnets map[string]int, i int, n vmNICModel, private bool) error {
	if !private || !knownString(n.SubnetID) {
		return nil
	}
	if j, dup := subnets[n.SubnetID.ValueString()]; dup {
		return fmt.Errorf(
			"network_interface[%d] and [%d] bind the same subnet; one private interface per subnet", j, i,
		)
	}
	subnets[n.SubnetID.ValueString()] = i
	return nil
}

// noteUnboundPrivate allows exactly one private block to omit subnet_id: that
// one keeps the cluster's default subnet, which the API assigns. A second
// interface has no such default to fall back on, and two unbound blocks could
// not be told apart.
func noteUnboundPrivate(nics []vmNICModel) error {
	unbound := -1
	for i, n := range nics {
		if nicIsPublic(n) || knownString(n.SubnetID) || n.SubnetID.IsUnknown() {
			continue
		}
		if unbound >= 0 {
			return fmt.Errorf(
				"network_interface[%d] and [%d] both omit subnet_id; only the default interface may", unbound, i,
			)
		}
		unbound = i
	}
	if unbound < 0 {
		return nil
	}
	for i, n := range nics {
		if i != unbound && nicIsDefault(n) {
			return fmt.Errorf(
				"network_interface[%d] omits subnet_id but network_interface[%d] is the default; "+
					"only the default interface may omit it", unbound, i,
			)
		}
	}
	return nil
}

// noteDefault records block i as the default when it claims to be, rejecting
// a public default or a second one.
func noteDefault(dflt, i int, n vmNICModel, private bool) (int, error) {
	if !nicIsDefault(n) {
		return dflt, nil
	}
	if !private {
		return -1, fmt.Errorf("network_interface[%d]: only a private interface can be the default", i)
	}
	if dflt >= 0 {
		return -1, fmt.Errorf("network_interface[%d] and [%d] are both default; exactly one is allowed", dflt, i)
	}
	return i, nil
}

func nicIsDefault(n vmNICModel) bool {
	return !n.Default.IsNull() && !n.Default.IsUnknown() && n.Default.ValueBool()
}

// validateNIC checks one block's shape and reports whether it is private.
func validateNIC(i int, n vmNICModel) (bool, error) {
	hasSubnet := knownString(n.SubnetID)
	hasStatic := !n.StaticIPs.IsNull() && !n.StaticIPs.IsUnknown() && len(n.StaticIPs.Elements()) > 0
	switch n.Type.ValueString() {
	case nicTypePrivate:
		if knownString(n.PublicIPID) || knownString(n.AddressType) {
			return false, fmt.Errorf(
				"network_interface[%d]: public_ip_id and address_type apply to public interfaces only",
				i,
			)
		}
		return true, nil
	case nicTypePublic:
		if hasSubnet {
			return false, fmt.Errorf("network_interface[%d]: subnet_id applies to private interfaces only", i)
		}
		if hasStatic {
			return false, fmt.Errorf("network_interface[%d]: static_ips apply to private interfaces only", i)
		}
		if knownString(n.PublicIPID) && knownString(n.AddressType) {
			// Both known in one block can only come from the configuration:
			// the plan stamps address_type null whenever public_ip_id is set.
			return false, fmt.Errorf(
				"network_interface[%d]: address_type applies only to a VM-created IP; drop it or public_ip_id",
				i,
			)
		}
		return false, nil
	default:
		return false, nil // type is validated by the schema
	}
}

// assembleDraftNICs realizes the plan's blocks on a draft VM: the server's
// default interface is repointed to the default block's subnet, the other
// blocks are added, then security groups and static IPs are set
// (graph @cloudless/fluence, node #1811).
func assembleDraftNICs(ctx context.Context, c *client.Client, vmID string, nics []vmNICModel) ([]string, error) {
	if len(nics) == 0 {
		return nil, nil
	}
	dflt, err := validateNICs(nics)
	if err != nil {
		return nil, err
	}
	if berr := bindDraftDefaultNIC(ctx, c, vmID, nics[dflt]); berr != nil {
		return nil, berr
	}
	var ownedIPs []string
	for i, n := range nics {
		if i == dflt || nicAttachesExistingIP(n) {
			// An existing public IP can only be attached to a live VM; see
			// attachExistingPublicNICs after provision.
			continue
		}
		iface, aerr := c.AddVMInterface(ctx, vmID, addRequestFor(n, true))
		if aerr != nil {
			return ownedIPs, fmt.Errorf("add interface %s: %w", nicKey(n), aerr)
		}
		if ipID := iface.PublicIPID(); ipID != "" && nicOwnsIP(n) {
			ownedIPs = append(ownedIPs, ipID)
		}
		if serr := applyNICSettings(ctx, c, vmID, iface.ID, n, true); serr != nil {
			return ownedIPs, serr
		}
	}
	return ownedIPs, nil
}

// bindDraftDefaultNIC repoints the server's default interface to the default
// block's subnet and applies that block's settings.
func bindDraftDefaultNIC(ctx context.Context, c *client.Client, vmID string, dflt vmNICModel) error {
	current, err := c.ListVMInterfaces(ctx, vmID)
	if err != nil {
		return fmt.Errorf("list draft interfaces: %w", err)
	}
	var serverDefault *client.VMInterface
	for i := range current {
		if current[i].Default {
			serverDefault = &current[i]
		}
	}
	if serverDefault == nil {
		return errors.New("draft has no default interface")
	}
	// A default block without subnet_id keeps whatever subnet the API gave
	// the draft — that is the point of leaving it out.
	want := dflt.SubnetID.ValueString()
	if knownString(dflt.SubnetID) && serverDefault.SubnetID() != want {
		if _, rerr := c.RepointVMInterface(ctx, vmID, serverDefault.ID, want); rerr != nil {
			return fmt.Errorf("bind default interface to subnet %s: %w", want, rerr)
		}
	}
	return applyNICSettings(ctx, c, vmID, serverDefault.ID, dflt, true)
}

// attachExistingPublicNICs attaches the blocks that reference an existing
// public IP to the now-live VM and restarts it if the API asks for it. The
// API forbids attaching an existing IP to a draft.
func attachExistingPublicNICs(ctx context.Context, c *client.Client, vmID string, nics []vmNICModel) error {
	attached := false
	for _, n := range nics {
		if !nicAttachesExistingIP(n) {
			continue
		}
		var iface *client.VMInterface
		aerr := retryInterfaceOpWith(ctx, true, func(ctx context.Context) error {
			var e error
			iface, e = c.AddVMInterface(ctx, vmID, addRequestFor(n, false))
			return e
		})
		if aerr != nil {
			return fmt.Errorf("attach public ip %s: %w", n.PublicIPID.ValueString(), aerr)
		}
		if serr := applyNICSettings(ctx, c, vmID, iface.ID, n, false); serr != nil {
			return serr
		}
		attached = true
	}
	if !attached {
		return nil
	}
	return restartIfFlagged(ctx, c, vmID)
}

// restartIfFlagged restarts the VM when the API asks for it, waiting for the
// VM to stop reconciling first: a flag read while it is still `updating` can
// be the value from before the change, and skipping the restart there leaves
// the change without effect.
func restartIfFlagged(ctx context.Context, c *client.Client, vmID string) error {
	vm, err := waitSettled(ctx, c, vmID)
	if err != nil {
		return err
	}
	if !vm.RestartRequired {
		return nil
	}
	return restartAndWaitReady(ctx, c, vmID)
}

// waitSettled polls the VM until its status is one the API will not move on
// its own, and returns that reading.
func waitSettled(ctx context.Context, c *client.Client, vmID string) (*client.VM, error) {
	var last *client.VM
	var blip transientWindow
	err := waitFor(ctx, interfacePoll(), func(ctx context.Context) error {
		vm, gerr := c.GetVM(ctx, vmID)
		if gerr != nil {
			return blip.absorb(gerr)
		}
		blip.clear()
		last = vm
		if isSettled(vm.Status) {
			return errStopPolling
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for vm %s to settle: %w", vmID, err)
	}
	return last, nil
}

func addRequestFor(n vmNICModel, draft bool) client.AddInterfaceRequest {
	if !nicIsPublic(n) {
		return client.AddInterfaceRequest{SubnetID: n.SubnetID.ValueString()}
	}
	req := client.AddInterfaceRequest{Public: true}
	if nicAttachesExistingIP(n) {
		req.PublicIPID = n.PublicIPID.ValueString()
		return req
	}
	if draft {
		req.AddressType = defaultPublicIPAddressType
		if knownString(n.AddressType) {
			req.AddressType = n.AddressType.ValueString()
		}
	}
	return req
}

// applyNICSettings sets the security group and (draft only) static IPs.
func applyNICSettings(ctx context.Context, c *client.Client, vmID, ifaceID string, n vmNICModel, draft bool) error {
	if !n.SecurityGroupID.IsNull() && !n.SecurityGroupID.IsUnknown() && n.SecurityGroupID.ValueString() != "" {
		sg := n.SecurityGroupID.ValueString()
		if err := c.SetVMInterfaceSecurityGroup(ctx, vmID, ifaceID, &sg); err != nil {
			return fmt.Errorf("bind security group %s: %w", sg, err)
		}
	}
	if draft && !n.StaticIPs.IsNull() && !n.StaticIPs.IsUnknown() && len(n.StaticIPs.Elements()) > 0 {
		if err := c.SetVMInterfaceStaticIPs(ctx, vmID, ifaceID, stringsFromList(n.StaticIPs)); err != nil {
			return fmt.Errorf("set static ips: %w", err)
		}
	}
	return nil
}

// reconcileLiveNICs brings a live VM's interfaces to the plan within what the
// API allows: add and remove non-default interfaces, change security-group
// bindings. Returns whether anything changed. Structural changes to matched
// interfaces are rejected by ModifyPlan as replacements before this runs
// (graph @cloudless/fluence, node #1812).
func reconcileLiveNICs(ctx context.Context, c *client.Client, vmID string, prev, next []vmNICModel) (bool, error) {
	current, err := c.ListVMInterfaces(ctx, vmID)
	if err != nil {
		return false, fmt.Errorf("list interfaces: %w", err)
	}
	keys, err := resolveNICKeys(prev, next)
	if err != nil {
		return false, err
	}
	wanted := map[string]bool{}
	for _, k := range keys {
		wanted[k] = true
	}

	// The repoint goes first and the list is taken again: the moved interface
	// answers to a different key afterwards, and the rest of the reconcile
	// matches blocks by key.
	moved, err := repointDefaultNIC(ctx, c, vmID, current, next)
	if err != nil {
		return false, err
	}
	if moved {
		if current, err = c.ListVMInterfaces(ctx, vmID); err != nil {
			return true, fmt.Errorf("list interfaces after repointing the default: %w", err)
		}
	}
	byKey := map[string]client.VMInterface{}
	for _, i := range current {
		byKey[apiNICKey(i)] = i
	}

	// Removals go first and are waited out, so an address released here is
	// free for whoever attaches it next (possibly another resource in the
	// same apply; graph @cloudless/fluence, node #1824).
	changed, err := removeUnwantedNICs(ctx, c, vmID, current, wanted)
	changed = changed || moved
	if err != nil {
		return changed, err
	}
	for idx, n := range next {
		did, eerr := ensureLiveNIC(ctx, c, vmID, keys[idx], n, byKey)
		if eerr != nil {
			return changed, eerr
		}
		changed = changed || did
	}
	return changed, nil
}

// resolveNICKeys names every planned block by what it binds to. Owned IPs
// pair with the state's owned IPs in order: their ID is computed, not planned.
func resolveNICKeys(prev, next []vmNICModel) ([]string, error) {
	ownedIDs := ownedPublicIPs(prev)
	owned := 0
	keys := make([]string, len(next))
	for idx, n := range next {
		key := nicKey(n)
		if key == "pub:new" {
			if owned >= len(ownedIDs) {
				return nil, errors.New(
					"a live VM cannot create a new owned public IP; attach an existing public_ip_id instead",
				)
			}
			key = "pub:" + ownedIDs[owned]
			owned++
		}
		keys[idx] = key
	}
	return keys, nil
}

// removeUnwantedNICs drops the interfaces the plan no longer names and waits
// for the API to let go of them. The private default interface always stays
// (the API refuses to drop it); a public interface may carry the default flag
// too — the API moves it there on attach and back on removal — and goes.
func removeUnwantedNICs(
	ctx context.Context,
	c *client.Client,
	vmID string,
	current []client.VMInterface,
	wanted map[string]bool,
) (bool, error) {
	var removedIDs []string
	for _, i := range current {
		if (i.Default && !i.IsPublic()) || wanted[apiNICKey(i)] {
			continue
		}
		rerr := retryInterfaceOp(ctx, func(ctx context.Context) error { return c.RemoveVMInterface(ctx, vmID, i.ID) })
		if rerr != nil && !client.IsNotFound(rerr) {
			return len(removedIDs) > 0, fmt.Errorf("remove interface %s: %w", apiNICKey(i), rerr)
		}
		removedIDs = append(removedIDs, i.ID)
	}
	if len(removedIDs) == 0 {
		return false, nil
	}
	return true, settleRemovedNICs(ctx, c, vmID, removedIDs)
}

// retryInterfaceOp runs an interface mutation on a live VM until the API
// accepts it: 406 means the VM is briefly transitional (right after another
// interface change or a restart), 409 means a reserved IP is still held by
// the VM it is moving from; both clear by waiting.
func retryInterfaceOp(ctx context.Context, op func(context.Context) error) error {
	return retryInterfaceOpWith(ctx, false, op)
}

// retryInterfaceOpWith is retryInterfaceOp with 409 optionally retried:
// only attaching a reserved IP has a 409 that clears by waiting (the VM it
// moves from releases it); any other conflict is final and surfaces at once.
func retryInterfaceOpWith(ctx context.Context, waitOnConflict bool, op func(context.Context) error) error {
	var last error
	var blip transientWindow
	err := waitFor(ctx, interfacePoll(), func(ctx context.Context) error {
		err := op(ctx)
		switch {
		case err == nil:
			return errStopPolling
		case isTransient(err):
			last = err
			return blip.absorb(err)
		case client.IsNotAcceptable(err) || (waitOnConflict && client.IsConflict(err)):
			blip.clear()
			last = err
			return nil
		default:
			return err
		}
	})
	return withLastAnswer(err, last)
}

// withLastAnswer folds the last retried answer into a timeout error, so a
// permanent refusal that was retried as "transitional" is not hidden behind
// a generic timeout.
func withLastAnswer(err, last error) error {
	if err != nil && last != nil {
		return fmt.Errorf("%w (last answer: %w)", err, last)
	}
	return err
}

// settleRemovedNICs waits until removed interfaces stop being listed on the
// VM, restarting it first when the API asks for it: on a live VM the removal
// is applied by the restart, and the released IP stays "attached" until then.
func settleRemovedNICs(ctx context.Context, c *client.Client, vmID string, removed []string) error {
	if rerr := restartIfFlagged(ctx, c, vmID); rerr != nil {
		return fmt.Errorf("restart after removing interfaces: %w", rerr)
	}
	gone := map[string]bool{}
	for _, id := range removed {
		gone[id] = true
	}
	var blip transientWindow
	return waitFor(ctx, interfacePoll(), func(ctx context.Context) error {
		ifaces, lerr := c.ListVMInterfaces(ctx, vmID)
		if lerr != nil {
			return blip.absorb(lerr)
		}
		blip.clear()
		for _, i := range ifaces {
			if gone[i.ID] {
				return nil // still listed; keep waiting
			}
		}
		return errStopPolling
	})
}

// ensureLiveNIC adds a missing interface or aligns the security group of an
// existing one; reports whether it changed anything.
func ensureLiveNIC(
	ctx context.Context,
	c *client.Client,
	vmID, key string,
	n vmNICModel,
	byKey map[string]client.VMInterface,
) (bool, error) {
	iface, ok := byKey[key]
	if !ok {
		// A reserved IP may still be held by the VM it is moving from while
		// that resource's update runs concurrently; the API answers 409 until
		// it is released, so retry within the poll budget.
		var added *client.VMInterface
		err := retryInterfaceOpWith(ctx, nicAttachesExistingIP(n), func(ctx context.Context) error {
			var aerr error
			added, aerr = c.AddVMInterface(ctx, vmID, addRequestFor(n, false))
			return aerr
		})
		if err != nil {
			return false, fmt.Errorf("add interface %s: %w", key, err)
		}
		return true, applyNICSettings(ctx, c, vmID, added.ID, n, false)
	}
	wantSG := nullableString(n.SecurityGroupID)
	if sameOptString(iface.SecurityGroupID, wantSG) {
		return false, nil
	}
	err := retryInterfaceOp(ctx, func(ctx context.Context) error {
		return c.SetVMInterfaceSecurityGroup(ctx, vmID, iface.ID, wantSG)
	})
	if err != nil {
		return false, fmt.Errorf("update security group on %s: %w", key, err)
	}
	return true, nil
}

func sameOptString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// nicsFromAPI maps the API's interfaces onto blocks, keeping the order of the
// previous blocks (matched by id, then by key) so unchanged configurations plan
// empty; the public_ip ownership block is not observable from the API and is
// carried over from the previous blocks (graph @cloudless/fluence, node #1815).
func nicsFromAPI(prev []vmNICModel, ifaces []client.VMInterface) []vmNICModel {
	// No blocks configured: the server default stays unmanaged and is only
	// mirrored in the VM-level computed lists. Import surfaces interfaces
	// through importNICs instead.
	if len(prev) == 0 {
		return nil
	}
	used := make([]bool, len(ifaces))
	out := make([]vmNICModel, 0, len(ifaces))
	for _, p := range prev {
		i := matchNIC(p, ifaces, used)
		if i < 0 {
			continue
		}
		used[i] = true
		out = append(out, nicFromAPI(ifaces[i], p, true))
	}
	for i, f := range ifaces {
		if !used[i] {
			out = append(out, nicFromAPI(f, vmNICModel{}, false))
		}
	}
	return out
}

// importNICs renders every interface of an imported VM as a block so the
// user can write matching configuration. Ownership of a public IP cannot be
// read from the API: imported public blocks carry no address_type, so the
// VM will not release that IP on destroy unless the user declares ownership
// by writing the block without public_ip_id.
func importNICs(ifaces []client.VMInterface) []vmNICModel {
	out := make([]vmNICModel, 0, len(ifaces))
	for _, f := range ifaces {
		out = append(out, nicFromAPI(f, vmNICModel{}, false))
	}
	return out
}

// matchNIC finds the unused API interface for a block: by id, then by key,
// then (for a VM-owned IP block) any public interface.
func matchNIC(p vmNICModel, ifaces []client.VMInterface, used []bool) int {
	byID := !p.ID.IsNull() && !p.ID.IsUnknown() && p.ID.ValueString() != ""
	for i, f := range ifaces {
		if !used[i] && byID && p.ID.ValueString() == f.ID {
			return i
		}
	}
	key := nicKey(p)
	for i, f := range ifaces {
		if !used[i] && key == apiNICKey(f) {
			return i
		}
	}
	if nicOwnsIP(p) {
		for i, f := range ifaces {
			if !used[i] && f.IsPublic() {
				return i
			}
		}
	}
	if !nicIsPublic(p) && !knownString(p.SubnetID) {
		// A private block that named no subnet is the default one: it belongs
		// to whichever interface the API marks as the VM's default.
		for i, f := range ifaces {
			if !used[i] && !f.IsPublic() && f.Default {
				return i
			}
		}
	}
	return -1
}

// nicFromAPI builds a block from an API interface. prev is the block it was
// matched to (zero value when unmatched): ownership of a public IP and static
// IPs the API does not echo are carried from it.
func nicFromAPI(f client.VMInterface, prev vmNICModel, matched bool) vmNICModel {
	n := vmNICModel{
		ID:              types.StringValue(f.ID),
		Type:            types.StringValue(nicTypePrivate),
		SubnetID:        types.StringNull(),
		PublicIPID:      types.StringNull(),
		AddressType:     types.StringNull(),
		SecurityGroupID: stringFromPtr(f.SecurityGroupID),
		StaticIPs:       types.ListNull(types.StringType),
		Default:         types.BoolValue(f.Default),
		AssignedIPs:     listFromStrings(f.AssignedIPs),
	}
	if f.IsPublic() {
		n.Type = types.StringValue(nicTypePublic)
		n.PublicIPID = types.StringValue(f.PublicIPID())
		if matched && nicOwnsIP(prev) {
			n.AddressType = types.StringValue(defaultPublicIPAddressType)
			if knownString(prev.AddressType) {
				n.AddressType = prev.AddressType
			}
		}
		return n
	}
	if sub := f.SubnetID(); sub != "" {
		n.SubnetID = types.StringValue(sub)
	}
	// static_ips is what the API reports; a configured list the API did not
	// apply must not be echoed back as if it had been.
	if len(f.StaticIPs) > 0 {
		n.StaticIPs = listFromStrings(f.StaticIPs)
	} else if matched && !prev.StaticIPs.IsNull() && !prev.StaticIPs.IsUnknown() && len(prev.StaticIPs.Elements()) == 0 {
		n.StaticIPs = prev.StaticIPs // keep an explicit empty list stable
	}
	return n
}

// ownedPublicIPs returns the IDs of public IPs the VM created for itself.
func ownedPublicIPs(nics []vmNICModel) []string {
	var out []string
	for _, n := range nics {
		if nicOwnsIP(n) && knownString(n.PublicIPID) {
			out = append(out, n.PublicIPID.ValueString())
		}
	}
	return out
}

// planNICReplacement decides whether the network_interface change needs a new
// VM: a matched interface changing static IPs or default, the default
// interface's subnet changing, or the set of VM-owned public IPs changing.
// Add/remove and security-group changes are in-place.
func planNICReplacement(prev, next []vmNICModel) (bool, string) {
	if countOwned(prev) != countOwned(next) {
		return true, "the set of VM-owned public IPs changed"
	}
	// Match by key plus occurrence so two blocks on one subnet stay apart;
	// owned public blocks key differently in plan and state and are guarded
	// by countOwned above.
	prevByKey := map[string][]vmNICModel{}
	for _, p := range prev {
		if !nicOwnsIP(p) {
			prevByKey[nicKey(p)] = append(prevByKey[nicKey(p)], p)
		}
	}
	seen := map[string]int{}
	for _, n := range next {
		if nicOwnsIP(n) {
			continue
		}
		k := nicKey(n)
		i := seen[k]
		seen[k] = i + 1
		if i >= len(prevByKey[k]) {
			continue
		}
		if why := matchedNICChange(prevByKey[k][i], n); why != "" {
			return true, why
		}
	}
	return false, ""
}

// newNICsWithStaticIPs names a block that is new to a live VM and asks for
// static IPs: the API sets those only while the VM is a draft, so the block
// cannot be honoured in place.
func newNICsWithStaticIPs(prev, next []vmNICModel) string {
	have := map[string]bool{}
	for _, p := range prev {
		have[nicKey(p)] = true
	}
	for i, n := range next {
		if !have[nicKey(n)] && !n.StaticIPs.IsNull() && !n.StaticIPs.IsUnknown() && len(n.StaticIPs.Elements()) > 0 {
			return fmt.Sprintf("network_interface[%d] adds static_ips to a live VM", i)
		}
	}
	return ""
}

func countOwned(nics []vmNICModel) int {
	n := 0
	for _, x := range nics {
		if nicOwnsIP(x) {
			n++
		}
	}
	return n
}

// matchedNICChange names the immutable field that differs between a state
// block and its planned counterpart, or "" when only mutable fields changed.
func matchedNICChange(p, n vmNICModel) string {
	if !nicIsPublic(p) && !p.Default.IsNull() && !n.Default.IsNull() && !n.Default.IsUnknown() &&
		p.Default.ValueBool() != n.Default.ValueBool() {
		return "the default interface changed"
	}
	if !listsEqual(p.StaticIPs, n.StaticIPs) {
		return "static_ips changed on an existing interface"
	}
	return ""
}

// repointDefaultNIC moves the VM's default interface to the subnet the plan
// names. A live VM changes its default subnet this way — a PATCH and a
// restart, not a new VM — as long as the target subnet is in the VM's own
// cluster; a subnet elsewhere would mean moving the VM, which the API cannot
// do. Reports whether it moved anything.
func repointDefaultNIC(
	ctx context.Context,
	c *client.Client,
	vmID string,
	current []client.VMInterface,
	next []vmNICModel,
) (bool, error) {
	want := plannedDefaultSubnet(next)
	if want == "" {
		return false, nil
	}
	var dflt *client.VMInterface
	for i := range current {
		if current[i].Default && !current[i].IsPublic() {
			dflt = &current[i]
		}
	}
	if dflt == nil || dflt.SubnetID() == want {
		return false, nil
	}
	rerr := retryInterfaceOp(ctx, func(ctx context.Context) error {
		_, err := c.RepointVMInterface(ctx, vmID, dflt.ID, want)
		return err
	})
	if rerr != nil {
		return false, fmt.Errorf(
			"repoint default interface to subnet %s: %w "+
				"(a subnet in another cluster would mean moving the vm, which needs a replacement: "+
				"terraform apply -replace)",
			want, rerr)
	}
	return true, nil
}

// plannedDefaultSubnet names the subnet the plan's default private block
// binds, or "" when the plan names no such block. `default` is computed, so a
// freshly written block carries no value and the layout rules name the
// default instead.
func plannedDefaultSubnet(nics []vmNICModel) string {
	dflt, err := validateNICLayout(nics, false)
	if err != nil || dflt < 0 {
		return ""
	}
	// Only a private default binds the VM's subnet; the API may also flag a
	// public interface as default, and that flag travels with the IP.
	if n := nics[dflt]; !nicIsPublic(n) && knownString(n.SubnetID) {
		return n.SubnetID.ValueString()
	}
	return ""
}

// nicKeysDiffer reports whether the set of interface keys changes between
// two block lists (owned IPs compare by count).
func nicKeysDiffer(prev, next []vmNICModel) bool {
	count := func(ns []vmNICModel) map[string]int {
		m := map[string]int{}
		for _, n := range ns {
			k := nicKey(n)
			if nicOwnsIP(n) {
				k = "pub:owned"
			}
			m[k]++
		}
		return m
	}
	a, b := count(prev), count(next)
	if len(a) != len(b) {
		return true
	}
	for k, v := range a {
		if b[k] != v {
			return true
		}
	}
	return false
}

func listsEqual(a, b types.List) bool {
	if a.IsNull() || a.IsUnknown() || b.IsNull() || b.IsUnknown() {
		return (a.IsNull() || a.IsUnknown()) == (b.IsNull() || b.IsUnknown())
	}
	as, bs := stringsFromList(a), stringsFromList(b)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// restartAndWaitReady restarts the VM and waits until it reports launched
// again. Right after an interface change the VM can be in a transitional
// state where the API answers 406; retry until it accepts the restart
// (graph @cloudless/fluence, node #1804 questions the 406 contract).
func restartAndWaitReady(ctx context.Context, c *client.Client, vmID string) error {
	var last error
	err := waitFor(ctx, interfacePoll(), func(ctx context.Context) error {
		_, rErr := c.RestartVM(ctx, vmID)
		switch {
		case rErr == nil:
			return errStopPolling
		case client.IsNotAcceptable(rErr):
			// The VM is transitional — often because someone else's restart
			// is running. That restart may clear the flag, so re-read before
			// asking again rather than stacking a second reboot on top.
			last = rErr
			if vm, gerr := c.GetVM(ctx, vmID); gerr == nil && isSettled(vm.Status) && !vm.RestartRequired {
				return errStopPolling
			}
			return nil
		case isTransient(rErr):
			last = rErr
			return nil
		default:
			return rErr
		}
	})
	if err = withLastAnswer(err, last); err != nil {
		return err
	}
	_, err = pollUntilReady(ctx,
		func(ctx context.Context) (*client.VM, error) { return c.GetVM(ctx, vmID) },
		func(v *client.VM) string { return v.Status },
		"vm "+vmID,
	)
	return err
}

// nicDiagnostic wraps a validation error as an attribute diagnostic.
func nicDiagnostic(err error) diag.Diagnostic {
	return diag.NewAttributeErrorDiagnostic(path.Root("network_interface"), "Invalid network_interface", err.Error())
}
