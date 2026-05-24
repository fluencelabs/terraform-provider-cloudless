package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"
)

func NewStorageResource() resource.Resource { return &storageResource{} }

type storageResource struct{ c *client.Client }

type storageModel struct {
	ID          types.String `tfsdk:"id"`
	ClusterID   types.String `tfsdk:"cluster_id"`
	Name        types.String `tfsdk:"name"`
	StorageType types.String `tfsdk:"storage_type"`
	VolumeGb    types.Int64  `tfsdk:"volume_gb"`
	Replicated  types.Bool   `tfsdk:"replicated"`
	OSImage     types.String `tfsdk:"os_image"`
	Status      types.String `tfsdk:"status"`
	Role        types.String `tfsdk:"role"`
	UserID      types.String `tfsdk:"user_id"`
	AttachedTo  types.List   `tfsdk:"attached_to"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *storageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage"
}

func (r *storageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A storage volume on a Fluence cluster.",
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
			"storage_type": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("NVME")},
			},
			"volume_gb": schema.Int64Attribute{
				Required:    true,
				Description: "Volume size in GB. In-place resizes via PATCH; the operation may be disruptive (VM restart, data migration) — see provider docs.",
			},
			"replicated": schema.BoolAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"os_image": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()},
				Description:   "URL of an OS image. Presence makes this a boot disk.",
			},
			"status":      schema.StringAttribute{Computed: true},
			"role":        schema.StringAttribute{Computed: true},
			"user_id":     schema.StringAttribute{Computed: true},
			"attached_to": schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":  schema.StringAttribute{Computed: true},
		},
	}
}

func (r *storageResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *storageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan storageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.c.CreateStorage(ctx, client.CreateStorageRequest{
		ClusterID:   plan.ClusterID.ValueString(),
		Name:        plan.Name.ValueString(),
		StorageType: plan.StorageType.ValueString(),
		VolumeGb:    uint32(plan.VolumeGb.ValueInt64()),
		Replicated:  plan.Replicated.ValueBool(),
		OSImage:     plan.OSImage.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Create storage failed", err.Error())
		return
	}

	id := out.ID
	out, err = pollUntilReady(ctx,
		func(ctx context.Context) (*client.Storage, error) { return r.c.GetStorage(ctx, id) },
		func(v *client.Storage) string { return v.Status },
		"storage "+id,
	)
	if err != nil {
		resp.Diagnostics.AddError("Waiting for storage failed", err.Error())
		return
	}

	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *storageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state storageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetStorage(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read storage failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fill(&state, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *storageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state storageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	upd := client.UpdateStorageRequest{}
	changed := false
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		upd.Name = &v
		changed = true
	}
	if !plan.VolumeGb.Equal(state.VolumeGb) {
		v := uint32(plan.VolumeGb.ValueInt64())
		upd.VolumeGb = &v
		changed = true
	}
	var out *client.Storage
	if changed {
		got, err := r.c.UpdateStorage(ctx, state.ID.ValueString(), upd)
		if err != nil {
			resp.Diagnostics.AddError("Update storage failed", err.Error())
			return
		}
		out = got
	} else {
		got, err := r.c.GetStorage(ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Read storage failed", err.Error())
			return
		}
		out = got
	}
	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *storageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state storageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	deleteAndWait(ctx, &resp.Diagnostics, id,
		r.c.DeleteStorage,
		func(ctx context.Context) (*client.Storage, error) { return r.c.GetStorage(ctx, id) },
		func(v *client.Storage) string { return v.Status },
		"storage",
	)
}

func (r *storageResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *storageResource) fill(m *storageModel, s *client.Storage) {
	m.ID = types.StringValue(s.ID)
	m.ClusterID = types.StringValue(s.ClusterID)
	m.Name = types.StringValue(s.Name)
	m.StorageType = types.StringValue(s.StorageType)
	m.VolumeGb = types.Int64Value(int64(s.VolumeGb))
	m.Status = types.StringValue(s.Status)
	m.Role = types.StringValue(s.Role)
	m.UserID = types.StringValue(s.UserID)
	attached := s.AttachedTo
	if attached == nil {
		attached = []string{}
	}
	vals := make([]attr.Value, len(attached))
	for i, v := range attached {
		vals[i] = types.StringValue(v)
	}
	m.AttachedTo = types.ListValueMust(types.StringType, vals)
	m.CreatedAt = types.StringValue(s.CreatedAt)
}
