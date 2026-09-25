package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// /v3 draft lifecycle. Since 0.14.0 a draft is created whole: one POST carries
// the cluster, SKU, name, keys, boot disk, data disks and interfaces, and the
// receipt names the resources the server created for it. ProvisionVM then
// turns the saved draft into a live VM.
// (graph @cloudless/fluence, node #1790)

// VMDraftRequest is PublicVmDraftRequest. An omitted field inherits the
// platform default once; omitted Interfaces means one automatic interface,
// while an empty slice asks for none.
type VMDraftRequest struct {
	ClusterID       string           `json:"clusterId,omitempty"`
	ConfigurationID string           `json:"configurationId,omitempty"`
	Name            string           `json:"name,omitempty"`
	SSHKeyIDs       []string         `json:"sshKeyIds,omitempty"`
	CloudInit       string           `json:"cloudInit,omitempty"`
	BootDisk        *DraftBootDisk   `json:"bootDisk,omitempty"`
	DataDisks       []DraftDataDisk  `json:"dataDisks,omitempty"`
	Interfaces      []DraftInterface `json:"interfaces,omitempty"`
}

// DraftBootDisk is PublicDraftBootDisk: an existing Ready storage, or a new
// disk built from an image source.
type DraftBootDisk struct {
	StorageID string
	VolumeGb  uint32
	Name      string
	Source    ImageSource
}

func (d DraftBootDisk) MarshalJSON() ([]byte, error) {
	if d.StorageID != "" {
		return json.Marshal(map[string]any{"kind": "existing", "storageId": d.StorageID})
	}
	out := map[string]any{"kind": "new", "volumeGb": d.VolumeGb, "source": d.Source}
	if d.Name != "" {
		out["name"] = d.Name
	}
	return json.Marshal(out)
}

// DraftDataDisk is PublicDraftDataDisk; only the existing-disk variant is
// built here — the provider attaches disks that cloudless_storage manages.
type DraftDataDisk struct {
	StorageID string
}

func (d DraftDataDisk) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"kind": "existing", "storageId": d.StorageID})
}

// ImageSource is the tagged PublicImageSource. Only the catalog variant is
// built here: the http variant also needs a bootMode, and the provider has
// nowhere to say one (graph @cloudless/fluence, node #1911).
type ImageSource struct {
	ImageID string
}

func (s ImageSource) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"type": "catalog", "imageId": s.ImageID})
}

// CatalogImage names a boot image by its catalog id.
func CatalogImage(id string) ImageSource { return ImageSource{ImageID: id} }

// DraftInterface is PublicDraftInterface: private binds a subnet, public
// creates an address of AddressType.
type DraftInterface struct {
	Public          bool
	SubnetID        string
	AddressType     string
	SecurityGroupID string
	StaticIPs       []string
	Default         bool
}

func (i DraftInterface) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if i.Default {
		out["default"] = true
	}
	if i.Public {
		out["kind"] = "public"
		if i.AddressType != "" {
			out["addressType"] = i.AddressType
		}
		return json.Marshal(out)
	}
	out["kind"] = "private"
	out["subnetId"] = i.SubnetID
	if i.SecurityGroupID != "" {
		out["securityGroupId"] = i.SecurityGroupID
	}
	if len(i.StaticIPs) > 0 {
		out["staticIps"] = i.StaticIPs
	}
	return json.Marshal(out)
}

// CreatedVM is the CreatedVm receipt: the draft's id, the status it was
// accepted in, and the resources the server created for it — the public IPs
// there are the ones a discarded draft must release.
type CreatedVM struct {
	ID               string      `json:"id"`
	AcceptedStatus   string      `json:"acceptedStatus"`
	CreatedResources ResourceIDs `json:"createdResources"`
}

type ResourceIDs struct {
	StorageIDs  []string `json:"storageIds"`
	PublicIPIDs []string `json:"publicIpIds"`
}

// CreateVMDraft creates a draft VM from one request (POST /v3/vms, 201). The
// Idempotency-Key header is required: it names one logical create, so a retry
// after a lost answer returns the original receipt instead of a second VM.
// Each call gets a fresh key — a Terraform create is never replayed by the
// provider itself.
func (c *Client) CreateVMDraft(ctx context.Context, req VMDraftRequest) (*CreatedVM, error) {
	var out CreatedVM
	headers := map[string]string{"Idempotency-Key": newIdempotencyKey()}
	if err := c.doWithHeaders(ctx, http.MethodPost, "/v3/vms", nil, headers, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func newIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A key only has to be unique per call; the clock is enough when the
		// random source is not available.
		return "tf-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// UpdateVMRequest is the body of PATCH /v3/vms/{id}: name and SKU. The SKU is
// immutable once provisioned; renaming follows the live VM's own state rules.
type UpdateVMRequest struct {
	Name            *string `json:"name,omitempty"`
	ConfigurationID *string `json:"configurationId,omitempty"`
}

func (c *Client) UpdateVM(ctx context.Context, id string, req UpdateVMRequest) (*VM, error) {
	var out VM
	if err := c.do(ctx, http.MethodPatch, "/v3/vms/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProvisionVM turns a draft into a live VM (POST /v3/vms/{id}/provision). The
// server prices the draft graph, checks the balance and moves the VM and its
// draft sub-resources Draft -> New. A successful answer does not mean the VM
// is running; readiness is polled afterwards.
func (c *Client) ProvisionVM(ctx context.Context, id string) (*VM, error) {
	var out VM
	if err := c.do(ctx, http.MethodPost, "/v3/vms/"+id+"/provision", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteVMDraft discards a draft VM and its draft sub-resources
// (DELETE /v3/vms/{id}). Allowed only while the VM is a draft.
func (c *Client) DeleteVMDraft(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v3/vms/"+id, nil, nil, nil)
}
