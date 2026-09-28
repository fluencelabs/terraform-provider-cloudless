package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// Pricing. POST /v3/vm-estimates prices a complete explicit specification
// without creating anything, which is what lets a cost show up in a plan.
// (graph @cloudless/fluence, node #1790)

// VMSpec is PublicVmSpec: everything the API needs to price a VM. It is not
// the draft request — the shapes are close but the interface variant here
// always carries `default`, and nothing is created.
type VMSpec struct {
	Name            string
	ClusterID       string
	ConfigurationID string
	SSHKeyIDs       []string
	CloudInit       string
	BootDisk        SpecBootDisk
	DataDisks       []SpecDataDisk
	Interfaces      []SpecInterface
}

func (s VMSpec) MarshalJSON() ([]byte, error) {
	keys := s.SSHKeyIDs
	if keys == nil {
		keys = []string{}
	}
	disks := s.DataDisks
	if disks == nil {
		disks = []SpecDataDisk{}
	}
	out := map[string]any{
		"name":            s.Name,
		"clusterId":       s.ClusterID,
		"configurationId": s.ConfigurationID,
		"sshKeyIds":       keys,
		"bootDisk":        s.BootDisk,
		"dataDisks":       disks,
		"interfaces":      s.Interfaces,
	}
	if s.CloudInit != "" {
		out["cloudInit"] = s.CloudInit
	}
	return json.Marshal(out)
}

// SpecBootDisk is PublicBootDisk: an existing storage, or a new disk of
// VolumeGb built from Source.
type SpecBootDisk struct {
	StorageID string
	VolumeGb  uint32
	Name      string
	Source    ImageSource
}

func (d SpecBootDisk) MarshalJSON() ([]byte, error) {
	if d.StorageID != "" {
		return json.Marshal(map[string]any{"kind": "existing", "storageId": d.StorageID})
	}
	out := map[string]any{"kind": "new", "volumeGb": d.VolumeGb, "source": d.Source}
	if d.Name != "" {
		out["name"] = d.Name
	}
	return json.Marshal(out)
}

// SpecDataDisk is PublicDataDisk: an existing storage, or a new disk to price.
type SpecDataDisk struct {
	StorageID  string
	VolumeGb   uint32
	Name       string
	Replicated bool
}

func (d SpecDataDisk) MarshalJSON() ([]byte, error) {
	if d.StorageID != "" {
		return json.Marshal(map[string]any{"kind": "existing", "storageId": d.StorageID})
	}
	out := map[string]any{"kind": "new", "volumeGb": d.VolumeGb, "replicated": d.Replicated}
	if d.Name != "" {
		out["name"] = d.Name
	}
	return json.Marshal(out)
}

// SpecInterface is PublicInterface. Unlike the draft variant it always states
// whether it is the default one.
type SpecInterface struct {
	Public          bool
	SubnetID        string
	PublicIPID      string
	AddressType     string
	SecurityGroupID string
	StaticIPs       []string
	Default         bool
}

func (i SpecInterface) MarshalJSON() ([]byte, error) {
	out := map[string]any{"default": i.Default}
	if i.SecurityGroupID != "" {
		out["securityGroupId"] = i.SecurityGroupID
	}
	if i.Public {
		out["kind"] = "public"
		// The two public variants are exclusive: an existing address is named
		// by id, a new one by the type it would be allocated as.
		if i.PublicIPID != "" {
			out["publicIpId"] = i.PublicIPID
			return json.Marshal(out)
		}
		out["addressType"] = i.AddressType
		return json.Marshal(out)
	}
	out["kind"] = "private"
	out["subnetId"] = i.SubnetID
	if len(i.StaticIPs) > 0 {
		out["staticIps"] = i.StaticIPs
	}
	return json.Marshal(out)
}

// VMEstimate is PublicVmEstimate. The amounts are decimal strings, not
// floats: a price is money, and rounding it through a float would be a lie
// the provider tells quietly.
type VMEstimate struct {
	HourlyTotal       string `json:"hourlyTotal"`
	HourlyIncremental string `json:"hourlyIncremental"`
	Currency          string `json:"currency"`
	CalculatedAt      string `json:"calculatedAt"`
}

// EstimateVM prices a specification without creating anything.
func (c *Client) EstimateVM(ctx context.Context, spec VMSpec) (*VMEstimate, error) {
	var out VMEstimate
	if err := c.do(ctx, http.MethodPost, "/v3/vm-estimates", nil, spec, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EstimateExistingVM prices a VM that already exists, draft or live
// (GET /v3/vms/{id}/estimate).
func (c *Client) EstimateExistingVM(ctx context.Context, vmID string) (*VMEstimate, error) {
	var out VMEstimate
	if err := c.do(ctx, http.MethodGet, "/v3/vms/"+vmID+"/estimate", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
