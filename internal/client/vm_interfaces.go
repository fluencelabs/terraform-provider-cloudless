package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// VM network interfaces. A VM owns a list of interfaces; each is either
// private (bound to a subnet) or public (a public IP modeled as an interface).
// Security groups and static IPs are per interface, not per VM. Since 0.14.0
// the view is flat — kind is a word, and the subnet, address and group are
// fields beside it.
// (graph @cloudless/fluence, node #1809)

// Interface kinds as PublicInterfaceKind names them. A draft interface that
// binds nothing yet is "unbound".
const (
	interfaceKindPrivate = "private"
	interfaceKindPublic  = "public"
)

// VMInterface mirrors PublicInterfaceView.
type VMInterface struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Default         bool     `json:"default"`
	SubnetIDValue   *string  `json:"subnetId"`
	PublicIPIDValue *string  `json:"publicIpId"`
	SecurityGroupID *string  `json:"securityGroupId"`
	DesiredIPs      []string `json:"desiredIps"`
	AssignedIPs     []string `json:"assignedIps"`
}

// IsPublic reports whether the interface carries a public IP.
func (i VMInterface) IsPublic() bool { return i.Kind == interfaceKindPublic }

// SubnetID returns the private interface's subnet, or "" for public or
// unbound interfaces.
func (i VMInterface) SubnetID() string {
	if i.SubnetIDValue == nil {
		return ""
	}
	return *i.SubnetIDValue
}

// PublicIPID returns the public interface's IP ID, or "".
func (i VMInterface) PublicIPID() string {
	if i.PublicIPIDValue == nil {
		return ""
	}
	return *i.PublicIPIDValue
}

// StaticIPs returns the addresses asked for on this interface.
func (i VMInterface) StaticIPs() []string { return i.DesiredIPs }

// ListVMInterfaces returns the VM's interfaces
// (GET /v3/vms/{id}/interfaces); works for drafts and live VMs alike.
func (c *Client) ListVMInterfaces(ctx context.Context, vmID string) ([]VMInterface, error) {
	var out collection[VMInterface]
	if err := c.do(ctx, http.MethodGet, "/v3/vms/"+vmID+"/interfaces", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
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
	return c.patchInterface(ctx, vmID, interfaceID, body)
}

// SetVMInterfaceSecurityGroup binds (or with nil, unbinds) a security group.
// An absent securityGroupId leaves the binding unchanged and an explicit null
// clears it, so the null form is always sent here.
func (c *Client) SetVMInterfaceSecurityGroup(ctx context.Context, vmID, interfaceID string, sgID *string) error {
	body := struct {
		SecurityGroupID *string `json:"securityGroupId"`
	}{SecurityGroupID: sgID}
	_, err := c.patchInterface(ctx, vmID, interfaceID, body)
	return err
}

// SetVMInterfaceStaticIPs sets a draft private interface's static IPs; an
// empty slice clears them. The API refuses staticIps beside a non-null
// security group, so the two are sent as separate patches.
func (c *Client) SetVMInterfaceStaticIPs(ctx context.Context, vmID, interfaceID string, ips []string) error {
	if ips == nil {
		ips = []string{}
	}
	body := struct {
		StaticIPs []string `json:"staticIps"`
	}{StaticIPs: ips}
	_, err := c.patchInterface(ctx, vmID, interfaceID, body)
	return err
}

func (c *Client) patchInterface(ctx context.Context, vmID, interfaceID string, body any) (*VMInterface, error) {
	var out VMInterface
	if err := c.do(ctx, http.MethodPatch, "/v3/vms/"+vmID+"/interfaces/"+interfaceID, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
