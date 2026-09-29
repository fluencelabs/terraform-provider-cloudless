package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewVMEstimateDataSource() datasource.DataSource { return &vmEstimateDS{} }

var errMissingBootDisk = errors.New(
	"boot_disk needs either storage_id, or volume_gb together with image_id",
)

type vmEstimateDS struct{ c *client.Client }

type vmEstimateModel struct {
	Name            types.String            `tfsdk:"name"`
	ClusterID       types.String            `tfsdk:"cluster_id"`
	ConfigurationID types.String            `tfsdk:"configuration_id"`
	SSHKeyIDs       types.List              `tfsdk:"ssh_key_ids"`
	BootDisk        *estimateBootDiskModel  `tfsdk:"boot_disk"`
	DataDisks       []estimateDataDiskModel `tfsdk:"data_disk"`
	NICs            []estimateNICModel      `tfsdk:"network_interface"`

	HourlyTotal       types.String `tfsdk:"hourly_total"`
	HourlyIncremental types.String `tfsdk:"hourly_incremental"`
	Currency          types.String `tfsdk:"currency"`
	CalculatedAt      types.String `tfsdk:"calculated_at"`
}

type estimateBootDiskModel struct {
	StorageID types.String `tfsdk:"storage_id"`
	VolumeGb  types.Int64  `tfsdk:"volume_gb"`
	ImageID   types.String `tfsdk:"image_id"`
}

type estimateDataDiskModel struct {
	StorageID  types.String `tfsdk:"storage_id"`
	VolumeGb   types.Int64  `tfsdk:"volume_gb"`
	Replicated types.Bool   `tfsdk:"replicated"`
}

type estimateNICModel struct {
	Type        types.String `tfsdk:"type"`
	SubnetID    types.String `tfsdk:"subnet_id"`
	PublicIPID  types.String `tfsdk:"public_ip_id"`
	AddressType types.String `tfsdk:"address_type"`
	Default     types.Bool   `tfsdk:"default"`
}

func (d *vmEstimateDS) Metadata(
	_ context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_vm_estimate"
}

func (d *vmEstimateDS) Schema(
	_ context.Context,
	_ datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Price a VM before creating it. The API costs a complete specification without allocating " +
			"anything, so the figure shows up in `terraform plan`. Amounts are hourly USD, as decimal strings.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name the VM would carry. The API prices a complete specification, so it asks for one.",
			},
			"cluster_id":       schema.StringAttribute{Required: true},
			"configuration_id": schema.StringAttribute{Required: true},
			"ssh_key_ids": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Keys the VM would carry. They cost nothing; the API asks for the field.",
			},
			"hourly_total": schema.StringAttribute{
				Computed:    true,
				Description: "Hourly cost of everything in the specification, including resources that already exist.",
			},
			"hourly_incremental": schema.StringAttribute{
				Computed: true,
				Description: "Hourly cost of what this specification would newly bill — " +
					"an existing disk or address it reuses is already billed and is left out.",
			},
			"currency":      schema.StringAttribute{Computed: true},
			"calculated_at": schema.StringAttribute{Computed: true},
		},
		Blocks: map[string]schema.Block{
			"boot_disk": schema.SingleNestedBlock{
				Description: "The boot disk to price: an existing storage_id, or volume_gb plus image_id.",
				Attributes: map[string]schema.Attribute{
					"storage_id": schema.StringAttribute{Optional: true},
					"volume_gb":  schema.Int64Attribute{Optional: true},
					"image_id":   schema.StringAttribute{Optional: true},
				},
			},
			"data_disk": schema.ListNestedBlock{
				Description: "Data disks to price: an existing storage_id, or volume_gb (+ replicated).",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"storage_id": schema.StringAttribute{Optional: true},
						"volume_gb":  schema.Int64Attribute{Optional: true},
						"replicated": schema.BoolAttribute{Optional: true},
					},
				},
			},
			"network_interface": schema.ListNestedBlock{
				Description: "Interfaces to price; at least one is required. A public one carries an address, " +
					"which is what makes it cost something.",
				Validators: []validator.List{listvalidator.SizeAtLeast(1)},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Required:    true,
							Description: `"private" or "public".`,
						},
						"subnet_id": schema.StringAttribute{Optional: true},
						"public_ip_id": schema.StringAttribute{
							Optional:    true,
							Description: "Price an address the account already holds.",
						},
						"address_type": schema.StringAttribute{
							Optional:    true,
							Description: "Address type of an address the VM would allocate (default V4).",
						},
						"default": schema.BoolAttribute{Optional: true},
					},
				},
			},
		},
	}
}

func (d *vmEstimateDS) Configure(
	_ context.Context,
	req datasource.ConfigureRequest,
	resp *datasource.ConfigureResponse,
) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *vmEstimateDS) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var m vmEstimateModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := estimateSpec(m)
	if err != nil {
		resp.Diagnostics.AddError("Invalid VM specification", err.Error())
		return
	}
	out, err := d.c.EstimateVM(ctx, spec)
	if err != nil {
		resp.Diagnostics.AddError("Estimate VM failed", err.Error())
		return
	}

	m.HourlyTotal = types.StringValue(out.HourlyTotal)
	m.HourlyIncremental = types.StringValue(out.HourlyIncremental)
	m.Currency = types.StringValue(out.Currency)
	m.CalculatedAt = types.StringValue(out.CalculatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// estimateSpec turns the configuration into the body the pricing endpoint
// wants, applying the same defaults a VM would get: the first private
// interface is the default one.
func estimateSpec(m vmEstimateModel) (client.VMSpec, error) {
	spec := client.VMSpec{
		Name:            m.Name.ValueString(),
		ClusterID:       m.ClusterID.ValueString(),
		ConfigurationID: m.ConfigurationID.ValueString(),
		SSHKeyIDs:       stringsFromList(m.SSHKeyIDs),
	}
	boot, err := estimateBootDisk(m.BootDisk)
	if err != nil {
		return spec, err
	}
	spec.BootDisk = boot

	for _, disk := range m.DataDisks {
		if knownString(disk.StorageID) {
			spec.DataDisks = append(
				spec.DataDisks,
				client.SpecDataDisk{StorageID: disk.StorageID.ValueString()},
			)
			continue
		}
		spec.DataDisks = append(spec.DataDisks, client.SpecDataDisk{
			VolumeGb:   uint32(disk.VolumeGb.ValueInt64()),
			Replicated: disk.Replicated.ValueBool(),
		})
	}

	spec.Interfaces = estimateInterfaces(m.NICs)
	return spec, nil
}

func estimateBootDisk(b *estimateBootDiskModel) (client.SpecBootDisk, error) {
	if b == nil {
		return client.SpecBootDisk{}, errMissingBootDisk
	}
	if knownString(b.StorageID) {
		return client.SpecBootDisk{StorageID: b.StorageID.ValueString()}, nil
	}
	if b.VolumeGb.IsNull() || !knownString(b.ImageID) {
		return client.SpecBootDisk{}, errMissingBootDisk
	}
	return client.SpecBootDisk{
		VolumeGb: uint32(b.VolumeGb.ValueInt64()),
		Source:   client.CatalogImage(b.ImageID.ValueString()),
	}, nil
}

// estimateInterfaces marks the default one the way a VM would: the block that
// says so, else the first private block.
func estimateInterfaces(nics []estimateNICModel) []client.SpecInterface {
	dflt := -1
	for i, n := range nics {
		if !n.Default.IsNull() && n.Default.ValueBool() {
			dflt = i
			break
		}
	}
	if dflt < 0 {
		for i, n := range nics {
			if n.Type.ValueString() != nicTypePublic {
				dflt = i
				break
			}
		}
	}
	out := make([]client.SpecInterface, 0, len(nics))
	for i, n := range nics {
		iface := client.SpecInterface{
			Public:     n.Type.ValueString() == nicTypePublic,
			SubnetID:   n.SubnetID.ValueString(),
			PublicIPID: n.PublicIPID.ValueString(),
			Default:    i == dflt,
		}
		if iface.Public && iface.PublicIPID == "" {
			iface.AddressType = defaultPublicIPAddressType
			if knownString(n.AddressType) {
				iface.AddressType = n.AddressType.ValueString()
			}
		}
		out = append(out, iface)
	}
	return out
}
