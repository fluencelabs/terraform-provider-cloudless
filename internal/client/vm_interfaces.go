package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// VM network interfaces. A VM owns a list of interfaces; each is either
// private (bound to a subnet) or public (a public IP modeled as an interface).
// Security groups and static IPs are per interface, not per VM.
// (graph @cloudless/fluence, node #1809)

// VMInterface mirrors VmNetworkInterfaceDto.
type VMInterface struct {
	ID              string        `json:"id"`
	Default         bool          `json:"default"`
	Kind            InterfaceKind `json:"kind"`
	SecurityGroupID *string       `json:"securityGroup,omitempty"`
	StaticIPs       []string      `json:"staticIps"`
	AssignedIPs     []string      `json:"assignedIps"`
}

// InterfaceKind is the NetworkInterfaceKindDto oneOf: exactly one of Private
// or Public is set. A private interface may have no subnet while the VM is a
// draft.
type InterfaceKind struct {
	Private *PrivateInterface `json:"private,omitempty"`
	Public  *PublicInterface  `json:"public,omitempty"`
}

type PrivateInterface struct {
	Subnet *string `json:"subnet"`
}

type PublicInterface struct {
	PublicIP string `json:"public_ip"`
}

// IsPublic reports whether the interface carries a public IP.
func (i VMInterface) IsPublic() bool { return i.Kind.Public != nil }

// SubnetID returns the private interface's subnet, or "" for public or
// unbound interfaces.
func (i VMInterface) SubnetID() string {
	if i.Kind.Private != nil && i.Kind.Private.Subnet != nil {
		return *i.Kind.Private.Subnet
	}
	return ""
}

// PublicIPID returns the public interface's IP ID, or "".
func (i VMInterface) PublicIPID() string {
	if i.Kind.Public != nil {
		return i.Kind.Public.PublicIP
	}
	return ""
}

// ListVMInterfaces returns the VM's interfaces (GET /v2/vms/{id}/interfaces);
// works for drafts and live VMs alike.
func (c *Client) ListVMInterfaces(ctx context.Context, vmID string) ([]VMInterface, error) {
	var out []VMInterface
	if err := c.do(ctx, http.MethodGet, "/v2/vms/"+vmID+"/interfaces", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddInterfaceRequest is the body of POST /v3/vms/{id}/interfaces. Exactly one
// of SubnetID (private) or the public form is used: PublicIPID attaches an
// existing IP (required on a live VM, forbidden on a draft); a public request
// without PublicIPID makes the draft create its own IP of AddressType.
type AddInterfaceRequest struct {
	SubnetID    string
	Public      bool
	PublicIPID  string
	AddressType string
}

func (r AddInterfaceRequest) MarshalJSON() ([]byte, error) {
	if !r.Public {
		return json.Marshal(map[string]any{
			"kind": map[string]any{"private": map[string]any{"subnet": r.SubnetID}},
		})
	}
	pub := map[string]any{}
	if r.PublicIPID != "" {
		pub["publicIp"] = r.PublicIPID
	}
	if r.AddressType != "" {
		pub["addressType"] = r.AddressType
	}
	return json.Marshal(map[string]any{"kind": map[string]any{"public": pub}})
}

// AddVMInterface adds an interface to a draft or live VM and returns it.
func (c *Client) AddVMInterface(ctx context.Context, vmID string, req AddInterfaceRequest) (*VMInterface, error) {
	var out VMInterface
	if err := c.do(ctx, http.MethodPost, "/v3/vms/"+vmID+"/interfaces", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveVMInterface removes an interface (DELETE /v3/vms/{id}/interfaces/{iface}).
// A public interface detaches its IP; the default interface cannot be removed.
func (c *Client) RemoveVMInterface(ctx context.Context, vmID, interfaceID string) error {
	return c.do(ctx, http.MethodDelete, "/v3/vms/"+vmID+"/interfaces/"+interfaceID, nil, nil, nil)
}

// RepointVMInterface moves a private interface to another subnet
// (PATCH /v3/vms/{id}/interfaces/{iface} with {subnet}); the only way to
// change a VM's default subnet.
func (c *Client) RepointVMInterface(ctx context.Context, vmID, interfaceID, subnetID string) (*VMInterface, error) {
	body := struct {
		Subnet string `json:"subnet"`
	}{Subnet: subnetID}
	var out VMInterface
	if err := c.do(ctx, http.MethodPatch, "/v3/vms/"+vmID+"/interfaces/"+interfaceID, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InterfaceSettings is the body of PATCH /v2/vms/{id}/interfaces/{iface}:
// the security group binding and, on a draft's private interface, the static
// private IPs. Nil fields are omitted and left unchanged.
type InterfaceSettings struct {
	SecurityGroupID *string  `json:"securityGroupId,omitempty"`
	StaticIPs       []string `json:"staticIps,omitempty"`
}

// SetVMInterfaceSecurityGroup binds (or with nil, unbinds) a security group.
// The API distinguishes "absent" from null, so the null form is sent
// explicitly.
func (c *Client) SetVMInterfaceSecurityGroup(ctx context.Context, vmID, interfaceID string, sgID *string) error {
	body := struct {
		SecurityGroupID *string `json:"securityGroupId"`
	}{SecurityGroupID: sgID}
	return c.do(ctx, http.MethodPatch, "/v2/vms/"+vmID+"/interfaces/"+interfaceID, nil, body, nil)
}

// SetVMInterfaceStaticIPs sets a draft private interface's static IPs; an
// empty slice clears them.
func (c *Client) SetVMInterfaceStaticIPs(ctx context.Context, vmID, interfaceID string, ips []string) error {
	if ips == nil {
		ips = []string{}
	}
	body := struct {
		StaticIPs []string `json:"staticIps"`
	}{StaticIPs: ips}
	return c.do(ctx, http.MethodPatch, "/v2/vms/"+vmID+"/interfaces/"+interfaceID, nil, body, nil)
}
