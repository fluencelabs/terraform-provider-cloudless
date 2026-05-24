package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewSSHKeyResource() resource.Resource { return &sshKeyResource{} }

// samePublicKeyBody reports whether two OpenSSH public keys share the same key
// material, ignoring any trailing comment and surrounding whitespace. Fluence
// dedups keys by body (algorithm + base64), so this is how we recognise a key
// that already exists under a different name/comment.
func samePublicKeyBody(a, b string) bool {
	fa := strings.Fields(a)
	fb := strings.Fields(b)
	// Need at least "<algorithm> <base64>" on both sides.
	if len(fa) < 2 || len(fb) < 2 {
		return false
	}
	return fa[0] == fb[0] && fa[1] == fb[1]
}

type sshKeyResource struct {
	c *client.Client
}

type sshKeyModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	UserID      types.String `tfsdk:"user_id"`
	Algorithm   types.String `tfsdk:"algorithm"`
	Fingerprint types.String `tfsdk:"fingerprint"`
}

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An SSH public key registered with Fluence and reusable across VMs.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable name shown in the Fluence UI and CLI. Changing forces replacement; the Fluence API has no rename endpoint.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"public_key": schema.StringAttribute{
				Required:    true,
				Description: "The OpenSSH-formatted public key (ECDSA / RSA / ED25519).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id":     schema.StringAttribute{Computed: true},
			"algorithm":   schema.StringAttribute{Computed: true},
			"fingerprint": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *sshKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.CreateSSHKey(ctx, client.CreateSSHKeyRequest{
		Name:      plan.Name.ValueString(),
		PublicKey: plan.PublicKey.ValueString(),
	})
	if err != nil {
		// Fluence dedups keys by body: if this material is already registered
		// (possibly under a different name/comment), adopt the existing key
		// instead of failing. We keep the config's name and public_key in state
		// and only take the server-assigned identity/computed fields.
		if !client.IsConflict(err) {
			resp.Diagnostics.AddError("Create SSH key failed", err.Error())
			return
		}
		existing, found, lookupErr := r.findKeyByBody(ctx, plan.PublicKey.ValueString())
		if lookupErr != nil {
			resp.Diagnostics.AddError("Create SSH key failed",
				err.Error()+"; could not look up the existing key to adopt: "+lookupErr.Error())
			return
		}
		if !found {
			// 409 but no matching key found — surface the original error.
			resp.Diagnostics.AddError("Create SSH key failed", err.Error())
			return
		}
		out = existing
	}

	plan.ID = types.StringValue(out.ID)
	plan.UserID = types.StringValue(out.UserID)
	plan.Algorithm = types.StringValue(out.Algorithm)
	plan.Fingerprint = types.StringValue(out.Fingerprint)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// findKeyByBody returns the registered SSH key whose material matches publicKey
// (comment-insensitive). The bool reports whether a match was found.
func (r *sshKeyResource) findKeyByBody(
	ctx context.Context,
	publicKey string,
) (*client.SSHKey, bool, error) {
	keys, err := r.c.ListSSHKeys(ctx)
	if err != nil {
		return nil, false, err
	}
	for i := range keys {
		if samePublicKeyBody(keys[i].PublicKey, publicKey) {
			return &keys[i], true, nil
		}
	}
	return nil, false, nil
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.GetSSHKey(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read SSH key failed", err.Error())
		return
	}

	// Name and public_key are write-once from the config's perspective (both
	// carry RequiresReplace, and the API has no rename endpoint). Reconcile them
	// from the API only when state has no value yet — i.e. on import. Otherwise
	// preserve the configured values: an adopted key may have a different name
	// or a comment-less body, and overwriting would cause a perpetual
	// replace-loop.
	if state.Name.IsNull() || state.Name.ValueString() == "" {
		state.Name = types.StringValue(out.Name)
	}
	if !samePublicKeyBody(out.PublicKey, state.PublicKey.ValueString()) {
		state.PublicKey = types.StringValue(out.PublicKey)
	}
	state.UserID = types.StringValue(out.UserID)
	state.Algorithm = types.StringValue(out.Algorithm)
	state.Fingerprint = types.StringValue(out.Fingerprint)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op writer. Both Required attributes (name, public_key) carry
// RequiresReplace plan modifiers, so any user-driven change forces recreate.
// This implementation only exists to satisfy the resource interface.
func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.c.DeleteSSHKey(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete SSH key failed", err.Error())
	}
}

func (r *sshKeyResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
