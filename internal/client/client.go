// Package client is a minimal HTTP client for the Fluence public API
// (https://api.fluence.dev). It covers only the endpoints the cloudless
// Terraform provider currently uses.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const DefaultEndpoint = "https://api.fluence.dev"

const (
	// defaultHTTPTimeout bounds every request the client makes.
	defaultHTTPTimeout = 60 * time.Second
	// maxErrorBodyLen caps how much of an undecodable response body is echoed
	// back in an error message.
	maxErrorBodyLen = 256
)

type Client struct {
	endpoint string
	apiKey   string
	http     *http.Client
	ua       string
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }
func WithUserAgent(ua string) Option       { return func(c *Client) { c.ua = ua } }

func New(endpoint, apiKey string, opts ...Option) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	c := &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		http:     &http.Client{Timeout: defaultHTTPTimeout},
		ua:       "terraform-provider-cloudless",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// APIError is returned for any non-2xx response. The Fluence ErrorBody schema
// has a single `error` string; we also keep status code and raw body for
// diagnostics.
type APIError struct {
	StatusCode int
	Message    string
	RawBody    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("fluence api: %d %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("fluence api: %d %s", e.StatusCode, http.StatusText(e.StatusCode))
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.StatusCode == http.StatusNotFound
	}
	return false
}

// IsConflict reports whether err is a 409 from the API (e.g. a resource that
// already exists).
func IsConflict(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.StatusCode == http.StatusConflict
	}
	return false
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.endpoint + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// The Fluence docs show `Authorization: X-API-KEY <key>`. The OpenAPI
	// spec also describes an apiKey scheme with header name `X-API-KEY`. We
	// send both so either gateway configuration works.
	req.Header.Set("Authorization", "X-API-KEY "+c.apiKey)
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("User-Agent", c.ua)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		ae := &APIError{StatusCode: resp.StatusCode, RawBody: string(respBody)}
		var eb struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &eb) == nil && eb.Error != "" {
			ae.Message = eb.Error
		}
		return ae
	}

	if out != nil && len(respBody) > 0 {
		if err = json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w (body=%q)", err, truncate(string(respBody), maxErrorBodyLen))
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------- Pagination ----------

// PaginationInfo mirrors the API's PaginationInfo schema.
type PaginationInfo struct {
	TotalRecords    uint64 `json:"totalRecords"`
	FilteredRecords uint64 `json:"filteredRecords"`
	TotalPages      uint32 `json:"totalPages"`
	CurrentPage     uint64 `json:"currentPage"`
	PerPage         uint64 `json:"perPage"`
}

// ---------- SSH keys ----------

type SSHKey struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	Name        string `json:"name"`
	PublicKey   string `json:"publicKey"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}

type CreateSSHKeyRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}

type sshKeysListResponse struct {
	Items      []SSHKey       `json:"items"`
	Pagination PaginationInfo `json:"pagination"`
}

func (c *Client) CreateSSHKey(ctx context.Context, req CreateSSHKeyRequest) (*SSHKey, error) {
	var out SSHKey
	if err := c.do(ctx, http.MethodPost, "/v1/ssh_keys", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSSHKey reads a single SSH key by ID. The list endpoint accepts an `ids`
// filter so we use that to look up by ID.
func (c *Client) GetSSHKey(ctx context.Context, id string) (*SSHKey, error) {
	q := url.Values{"ids": {id}}
	var resp sshKeysListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/ssh_keys", q, nil, &resp); err != nil {
		return nil, err
	}
	for i := range resp.Items {
		if resp.Items[i].ID == id {
			return &resp.Items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "ssh key not found"}
}

// ListSSHKeys returns all SSH keys registered for the authenticated user. Used
// to recover from a create conflict by matching an existing key by body.
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	var resp sshKeysListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/ssh_keys", nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/ssh_keys/delete", nil, idsBody{IDs: []string{id}}, nil)
}

type idsBody struct {
	IDs []string `json:"ids"`
}

// ---------- VPCs ----------

type VPC struct {
	ID             string  `json:"id"`
	UserID         string  `json:"userId"`
	ClusterID      string  `json:"clusterId"`
	Name           string  `json:"name"`
	EnableExternal *bool   `json:"enableExternal,omitempty"`
	Status         string  `json:"status"`
	SubnetsCount   uint32  `json:"subnetsCount"`
	CreatedAt      string  `json:"createdAt"`
	ReadySince     *string `json:"readySince,omitempty"`
	RemovedAt      *string `json:"removedAt,omitempty"`
}

type CreateVPCRequest struct {
	ClusterID      string `json:"clusterId"`
	Name           string `json:"name"`
	EnableExternal *bool  `json:"enableExternal,omitempty"`
}

type UpdateVPCRequest struct {
	Name           *string `json:"name,omitempty"`
	EnableExternal *bool   `json:"enableExternal,omitempty"`
}

type vpcsListResponse struct {
	Items      []VPC          `json:"items"`
	Pagination PaginationInfo `json:"pagination"`
}

func (c *Client) CreateVPC(ctx context.Context, req CreateVPCRequest) (*VPC, error) {
	var out VPC
	if err := c.do(ctx, http.MethodPost, "/v1/vpcs", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetVPC(ctx context.Context, id string) (*VPC, error) {
	q := url.Values{"ids": {id}}
	var resp vpcsListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/vpcs", q, nil, &resp); err != nil {
		return nil, err
	}
	for i := range resp.Items {
		if resp.Items[i].ID == id {
			return &resp.Items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "vpc not found"}
}

func (c *Client) UpdateVPC(ctx context.Context, id string, req UpdateVPCRequest) (*VPC, error) {
	var out VPC
	if err := c.do(ctx, http.MethodPatch, "/v1/vpcs/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteVPC(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/vpcs/delete", nil, idsBody{IDs: []string{id}}, nil)
}

// ---------- Subnets ----------

type Subnet struct {
	ID         string  `json:"id"`
	UserID     string  `json:"userId"`
	ClusterID  string  `json:"clusterId"`
	VPCID      string  `json:"vpcId"`
	Name       string  `json:"name"`
	IPv4CIDR   *string `json:"ipv4Cidr,omitempty"`
	IPv6CIDR   *string `json:"ipv6Cidr,omitempty"`
	Status     string  `json:"status"`
	ReadySince *string `json:"readySince,omitempty"`
	RemovedAt  *string `json:"removedAt,omitempty"`
}

type CreateSubnetRequest struct {
	ClusterID string  `json:"clusterId"`
	Name      string  `json:"name"`
	IPv4CIDR  *string `json:"ipv4Cidr,omitempty"`
	IPv6CIDR  *string `json:"ipv6Cidr,omitempty"`
}

type UpdateSubnetRequest struct {
	Name *string `json:"name,omitempty"`
}

type subnetsListResponse struct {
	Items      []Subnet       `json:"items"`
	Pagination PaginationInfo `json:"pagination"`
}

func (c *Client) CreateSubnet(ctx context.Context, vpcID string, req CreateSubnetRequest) (*Subnet, error) {
	var out Subnet
	if err := c.do(ctx, http.MethodPost, "/v1/vpc/"+vpcID+"/subnets", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSubnet(ctx context.Context, id string) (*Subnet, error) {
	q := url.Values{"ids": {id}}
	var resp subnetsListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/subnets", q, nil, &resp); err != nil {
		return nil, err
	}
	for i := range resp.Items {
		if resp.Items[i].ID == id {
			return &resp.Items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "subnet not found"}
}

func (c *Client) UpdateSubnet(ctx context.Context, id string, req UpdateSubnetRequest) (*Subnet, error) {
	var out Subnet
	if err := c.do(ctx, http.MethodPatch, "/v1/subnets/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteSubnet(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/subnets/delete", nil, idsBody{IDs: []string{id}}, nil)
}

// ---------- VMs ----------

type VM struct {
	ID                string   `json:"id"`
	UserID            string   `json:"userId"`
	ClusterID         string   `json:"clusterId"`
	ConfigurationID   string   `json:"configurationId"`
	Name              string   `json:"name"`
	Status            string   `json:"status"`
	RestartRequired   bool     `json:"restartRequired"`
	BootDisk          *string  `json:"bootDisk,omitempty"`
	DataDisks         []string `json:"dataDisks"`
	Subnets           []string `json:"subnets"`
	SSHKeys           []string `json:"sshKeys"`
	NetworkInterfaces []string `json:"networkInterfaces"`
	PublicIP          *string  `json:"publicIp,omitempty"`
	ReadySince        *string  `json:"readySince,omitempty"`
	LastPaidAt        *string  `json:"lastPaidAt,omitempty"`
	PaidUntil         *string  `json:"paidUntil,omitempty"`
	TerminatedAt      *string  `json:"terminatedAt,omitempty"`
	CreatedAt         string   `json:"createdAt"`
	UpdatedAt         string   `json:"updatedAt"`
}

// VMBootDisk and VMDataDisk follow the OpenAPI `oneOf` pattern: either an
// existing storage ID (string) or a CreateUserStorageRequest. The provider
// only supports the inline-create form for now; users who want to attach
// existing volumes can add storage attachment as a follow-up.
type VMBootDisk struct {
	StorageID *string                  `json:"-"`
	Create    *CreateUserStorageInline `json:"-"`
}

func (b VMBootDisk) MarshalJSON() ([]byte, error) {
	if b.StorageID != nil {
		return json.Marshal(*b.StorageID)
	}
	return json.Marshal(b.Create)
}

type CreateUserStorageInline struct {
	ClusterID   string `json:"clusterId"`
	Name        string `json:"name"`
	StorageType string `json:"storageType"`
	VolumeGb    uint32 `json:"volumeGb"`
	Replicated  bool   `json:"replicated"`
	OSImage     string `json:"osImage,omitempty"`
}

type CreateVMRequest struct {
	ClusterID       string     `json:"clusterId"`
	Name            string     `json:"name"`
	ConfigurationID string     `json:"configurationId"`
	BootDisk        VMBootDisk `json:"bootDisk"`
	DataDisks       []string   `json:"dataDisks,omitempty"`
	SSHKeys         []string   `json:"sshKeys,omitempty"`
}

type UpdateVMRequest struct {
	Name *string `json:"name,omitempty"`
}

type vmsListResponse struct {
	Items      []VM           `json:"items"`
	Pagination PaginationInfo `json:"pagination"`
}

func (c *Client) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	var out VM
	if err := c.do(ctx, http.MethodPost, "/v2/vms", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetVM(ctx context.Context, id string) (*VM, error) {
	q := url.Values{"ids": {id}}
	var resp vmsListResponse
	if err := c.do(ctx, http.MethodGet, "/v2/vms", q, nil, &resp); err != nil {
		return nil, err
	}
	for i := range resp.Items {
		if resp.Items[i].ID == id {
			return &resp.Items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "vm not found"}
}

func (c *Client) UpdateVM(ctx context.Context, id string, req UpdateVMRequest) (*VM, error) {
	var out VM
	if err := c.do(ctx, http.MethodPatch, "/v2/vms/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) TerminateVM(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v2/vms/"+id+"/terminate", nil, nil, nil)
}

type vmStoragesBody struct {
	DataDisks []string `json:"dataDisks"`
}

func (c *Client) AddVMStorages(ctx context.Context, vmID string, storageIDs []string) error {
	return c.do(ctx, http.MethodPost, "/v2/vms/"+vmID+"/storages/add", nil, vmStoragesBody{DataDisks: storageIDs}, nil)
}

func (c *Client) RemoveVMStorages(ctx context.Context, vmID string, storageIDs []string) error {
	return c.do(
		ctx,
		http.MethodPost,
		"/v2/vms/"+vmID+"/storages/remove",
		nil,
		vmStoragesBody{DataDisks: storageIDs},
		nil,
	)
}

func (c *Client) AddVMPublicIP(ctx context.Context, vmID, publicIPID string) error {
	body := struct {
		PublicIPID string `json:"publicIpId"`
	}{PublicIPID: publicIPID}
	return c.do(ctx, http.MethodPost, "/v2/vms/"+vmID+"/public_ip/add", nil, body, nil)
}

func (c *Client) RemoveVMPublicIP(ctx context.Context, vmID string) error {
	return c.do(ctx, http.MethodPost, "/v2/vms/"+vmID+"/public_ip/remove", nil, nil, nil)
}

// VMNetworkInterface is one of the network interfaces attached to a VM. Today
// the provider only needs the ID + the optional security group binding so the
// security-group attachment resource can manage it.
type VMNetworkInterface struct {
	ID              string  `json:"id"`
	SecurityGroupID *string `json:"securityGroupId,omitempty"`
}

func (c *Client) ListVMInterfaces(ctx context.Context, vmID string) ([]VMNetworkInterface, error) {
	var out []VMNetworkInterface
	if err := c.do(ctx, http.MethodGet, "/v2/vms/"+vmID+"/interfaces", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) UpdateVMInterface(ctx context.Context, vmID, interfaceID string, securityGroupID *string) error {
	body := struct {
		SecurityGroupID *string `json:"securityGroupId"`
	}{SecurityGroupID: securityGroupID}
	return c.do(ctx, http.MethodPatch, "/v2/vms/"+vmID+"/interfaces/"+interfaceID, nil, body, nil)
}

// FindVMByInterface lists VMs and returns the one whose networkInterfaces
// contains the given interface_id. Returns *APIError 404 if no VM owns it.
//
// TODO(fluence-api): replace with /v2/vms?interfaces=<id> when the API
// adds the filter — this scan is O(N) over the user's whole fleet.
func (c *Client) FindVMByInterface(ctx context.Context, interfaceID string) (*VM, error) {
	// The API doesn't expose an interface→vm filter. Walk the user's VMs in
	// pages, indexing pages 1..N as the server reports them.
	const maxIters = 10000 // ~2M VMs at per_page=200; defensive cap if pagination metadata never converges.
	nextPage := uint64(1)
	for range maxIters {
		q := url.Values{"page": {FormatPage(nextPage)}, "per_page": {"200"}}
		var resp vmsListResponse
		if err := c.do(ctx, http.MethodGet, "/v2/vms", q, nil, &resp); err != nil {
			return nil, err
		}
		for i := range resp.Items {
			if slices.Contains(resp.Items[i].NetworkInterfaces, interfaceID) {
				return &resp.Items[i], nil
			}
		}
		if resp.Pagination.CurrentPage >= uint64(resp.Pagination.TotalPages) {
			return nil, &APIError{StatusCode: http.StatusNotFound, Message: "no VM owns interface " + interfaceID}
		}
		nextPage = resp.Pagination.CurrentPage + 1
	}
	return nil, fmt.Errorf("FindVMByInterface: pagination did not terminate after %d iterations", maxIters)
}

// ---------- Hardware (data sources) ----------

type Cluster struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// DCID is the only snake_case field in the Cluster wire shape — the
	// Fluence API spec is inconsistent here. Don't "fix" to dcId.
	DCID string `json:"dc_id"`
}

func (c *Client) ListClusters(ctx context.Context) ([]Cluster, error) {
	var out []Cluster
	if err := c.do(ctx, http.MethodGet, "/v1/clusters", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type VMConfiguration struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	VCPU        uint8    `json:"vcpu"`
	RAMGb       uint64   `json:"ramGb"`
	Dedicated   bool     `json:"dedicated"`
	CPUFamilies []string `json:"cpuFamilies"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
}

func (c *Client) ListVMConfigurations(ctx context.Context) ([]VMConfiguration, error) {
	var out []VMConfiguration
	if err := c.do(ctx, http.MethodGet, "/v1/configurations/virtual_machines", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type DefaultImage struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Distribution string `json:"distribution"`
	DownloadURL  string `json:"downloadUrl"`
	Username     string `json:"username"`
	IconURL      string `json:"iconUrl"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

func (c *Client) ListDefaultImages(ctx context.Context) ([]DefaultImage, error) {
	var out []DefaultImage
	if err := c.do(ctx, http.MethodGet, "/v1/storages/default_images", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------- Datacenters ----------

type Datacenter struct {
	ID             string   `json:"id"`
	CountryCode    string   `json:"countryCode"`
	CityCode       string   `json:"cityCode"`
	Index          int32    `json:"index"`
	Tier           int32    `json:"tier"`
	Certifications []string `json:"certifications"`
	Slug           string   `json:"slug"`
}

func (c *Client) ListDatacenters(ctx context.Context) ([]Datacenter, error) {
	var out []Datacenter
	if err := c.do(ctx, http.MethodGet, "/v1/datacenters", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// EnrichedCluster is a Cluster joined with its Datacenter. Used by data
// sources that want to expose region (countryCode) and city without forcing
// callers to do the join themselves.
type EnrichedCluster struct {
	Cluster

	Region           string // = Datacenter.CountryCode
	CityCode         string
	DCSlug           string
	DCTier           int32
	DCCertifications []string
}

// ListEnrichedClusters fetches both endpoints and joins them in memory.
func (c *Client) ListEnrichedClusters(ctx context.Context) ([]EnrichedCluster, error) {
	clusters, err := c.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	dcs, err := c.ListDatacenters(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]Datacenter{}
	for _, d := range dcs {
		byID[d.ID] = d
	}
	out := make([]EnrichedCluster, 0, len(clusters))
	for _, cl := range clusters {
		ec := EnrichedCluster{Cluster: cl}
		if d, ok := byID[cl.DCID]; ok {
			ec.Region = d.CountryCode
			ec.CityCode = d.CityCode
			ec.DCSlug = d.Slug
			ec.DCTier = d.Tier
			ec.DCCertifications = d.Certifications
		}
		out = append(out, ec)
	}
	return out, nil
}

// ---------- Helpers ----------

// FormatPage formats a 0-based page number as a query value the API accepts.
func FormatPage(p uint64) string { return strconv.FormatUint(p, 10) }

// ---------- Security groups ----------

type SecurityGroup struct {
	ID           string             `json:"id"`
	UserID       string             `json:"userId"`
	ClusterID    string             `json:"clusterId"`
	VPCID        string             `json:"vpcId"`
	Name         string             `json:"name"`
	Status       string             `json:"status"`
	IngressRules SecurityGroupRules `json:"ingressRules"`
	EgressRules  SecurityGroupRules `json:"egressRules"`
	AttachedTo   []string           `json:"attachedTo"`
	CreatedAt    string             `json:"createdAt"`
}

// SecurityGroupRules mirrors the API's discriminated union. We carry it as a
// struct with explicit Mode + Rules; helpers convert to/from wire JSON.
type SecurityGroupRules struct {
	// Mode is "allowAll" or "allow" (with possibly empty Rules).
	Mode  string              `json:"-"`
	Rules []SecurityGroupRule `json:"-"`
}

func (r SecurityGroupRules) MarshalJSON() ([]byte, error) {
	switch r.Mode {
	case "allowAll":
		return json.Marshal(map[string]string{"type": "allowAll"})
	case "allow":
		return json.Marshal(map[string]any{"type": "allow", "rules": r.Rules})
	default:
		return nil, fmt.Errorf("unknown SecurityGroupRules mode: %q", r.Mode)
	}
}

func (r *SecurityGroupRules) UnmarshalJSON(b []byte) error {
	var probe struct {
		Type  string              `json:"type"`
		Rules []SecurityGroupRule `json:"rules"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	r.Mode = probe.Type
	r.Rules = probe.Rules
	return nil
}

// SecurityGroupRule wraps the discriminated rule union (ipv4 vs ipv6) plus
// protocolKind and remote.
type SecurityGroupRule struct {
	Type         string       `json:"type"` // "ipv4" or "ipv6"
	ProtocolKind ProtocolKind `json:"protocolKind"`
	Remote       SGRemote     `json:"remote"`
}

// ProtocolKind is the protocolKind discriminated union.
// Encodings:
//   - "all"  → bare string "all"
//   - "icmp" → bare string "icmp"
//   - "tcp"  → {"tcp": {"ports": <Ports>}}
//   - "udp"  → {"udp": {"ports": <Ports>}}
type ProtocolKind struct {
	Kind  string // "all", "icmp", "tcp", "udp"
	Ports Ports  // unused for all/icmp
}

func (p ProtocolKind) MarshalJSON() ([]byte, error) {
	switch p.Kind {
	case "all", "icmp":
		return json.Marshal(p.Kind)
	case "tcp":
		return json.Marshal(map[string]any{"tcp": map[string]any{"ports": p.Ports}})
	case "udp":
		return json.Marshal(map[string]any{"udp": map[string]any{"ports": p.Ports}})
	default:
		return nil, fmt.Errorf("unknown ProtocolKind: %q", p.Kind)
	}
}

func (p *ProtocolKind) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		p.Kind = s
		return nil
	}
	var obj map[string]struct {
		Ports Ports `json:"ports"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	for k, v := range obj {
		p.Kind = k
		p.Ports = v.Ports
		return nil
	}
	return errors.New("ProtocolKind: empty object")
}

// Ports is "all" or {exact:{value:N}} or {range:{min:M,max:N}}.
type Ports struct {
	All      bool
	Exact    *uint16
	RangeMin *uint16
	RangeMax *uint16
}

func (p Ports) MarshalJSON() ([]byte, error) {
	if p.All {
		return json.Marshal("all")
	}
	if p.Exact != nil {
		return json.Marshal(map[string]any{"exact": map[string]any{"value": *p.Exact}})
	}
	if p.RangeMin != nil && p.RangeMax != nil {
		return json.Marshal(map[string]any{"range": map[string]any{"min": *p.RangeMin, "max": *p.RangeMax}})
	}
	return nil, errors.New("Ports: empty value")
}

func (p *Ports) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil && s == "all" {
		p.All = true
		return nil
	}
	type rangePart struct {
		Min uint16 `json:"min"`
		Max uint16 `json:"max"`
	}
	var probe struct {
		Exact *struct {
			Value uint16 `json:"value"`
		} `json:"exact"`
		Range *rangePart `json:"range"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	if probe.Exact != nil {
		v := probe.Exact.Value
		p.Exact = &v
	}
	if probe.Range != nil {
		mn, mx := probe.Range.Min, probe.Range.Max
		p.RangeMin, p.RangeMax = &mn, &mx
	}
	return nil
}

// SGRemote is "address" (CIDR) or "securityGroup" (SG ID reference).
type SGRemote struct {
	Address       *string
	SecurityGroup *string
}

func (r SGRemote) MarshalJSON() ([]byte, error) {
	if r.Address != nil {
		return json.Marshal(map[string]string{"address": *r.Address})
	}
	if r.SecurityGroup != nil {
		return json.Marshal(map[string]string{"securityGroup": *r.SecurityGroup})
	}
	return nil, errors.New("SGRemote: empty")
}

func (r *SGRemote) UnmarshalJSON(b []byte) error {
	var probe struct {
		Address       *string `json:"address,omitempty"`
		SecurityGroup *string `json:"securityGroup,omitempty"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	r.Address = probe.Address
	r.SecurityGroup = probe.SecurityGroup
	return nil
}

// CreateSecurityGroupRequest mirrors vodopad's CreateUserSecurityGroupRequest.
// Note the create endpoint models each direction as Option<Vec<…>> — an array
// or an absent field — NOT the {type, rules} object used by reads and updates.
type CreateSecurityGroupRequest struct {
	ClusterID    string               `json:"clusterId"`
	Name         string               `json:"name"`
	IngressRules *[]SecurityGroupRule `json:"ingressRules,omitempty"`
	EgressRules  *[]SecurityGroupRule `json:"egressRules,omitempty"`
}

// RulesToCreateField maps the internal SecurityGroupRules (the object form
// shared with reads and updates) onto the create endpoint's per-direction wire
// shape Option<Vec<SecurityGroupRuleDto>>:
//   - AllowAll        -> nil       (field omitted; the API reads None = allow all)
//   - Allow, no rules -> &[]       (empty array = deny all)
//   - Allow, rules    -> &[rule…]  (allow-listed)
func RulesToCreateField(r SecurityGroupRules) *[]SecurityGroupRule {
	if r.Mode == "allowAll" {
		return nil
	}
	rules := r.Rules
	if rules == nil {
		rules = []SecurityGroupRule{}
	}
	return &rules
}

type UpdateSecurityGroupRequest struct {
	Name         *string             `json:"name,omitempty"`
	IngressRules *SecurityGroupRules `json:"ingressRules,omitempty"`
	EgressRules  *SecurityGroupRules `json:"egressRules,omitempty"`
}

type sgListResponse struct {
	Items      []SecurityGroup `json:"items"`
	Pagination PaginationInfo  `json:"pagination"`
}

func (c *Client) CreateSecurityGroup(ctx context.Context, req CreateSecurityGroupRequest) (*SecurityGroup, error) {
	var out SecurityGroup
	if err := c.do(ctx, http.MethodPost, "/v1/security_groups", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSecurityGroup(ctx context.Context, id string) (*SecurityGroup, error) {
	q := url.Values{"ids": {id}}
	var resp sgListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/security_groups", q, nil, &resp); err != nil {
		return nil, err
	}
	for i := range resp.Items {
		if resp.Items[i].ID == id {
			return &resp.Items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "security group not found"}
}

func (c *Client) UpdateSecurityGroup(
	ctx context.Context,
	id string,
	req UpdateSecurityGroupRequest,
) (*SecurityGroup, error) {
	var out SecurityGroup
	if err := c.do(ctx, http.MethodPatch, "/v1/security_groups/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteSecurityGroup(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/security_groups/delete", nil, idsBody{IDs: []string{id}}, nil)
}

// ---------- Storage ----------

type Storage struct {
	ID          string   `json:"id"`
	UserID      string   `json:"userId"`
	ClusterID   string   `json:"clusterId"`
	Name        string   `json:"name"`
	StorageType string   `json:"storageType"`
	Status      string   `json:"status"`
	Role        string   `json:"role"`
	VolumeGb    uint64   `json:"volumeGb"`
	AttachedTo  []string `json:"attachedTo"`
	CreatedAt   string   `json:"createdAt"`
}

type CreateStorageRequest struct {
	ClusterID   string `json:"clusterId"`
	Name        string `json:"name"`
	StorageType string `json:"storageType"`
	VolumeGb    uint32 `json:"volumeGb"`
	Replicated  bool   `json:"replicated"`
	OSImage     string `json:"osImage,omitempty"`
}

type UpdateStorageRequest struct {
	Name     *string `json:"name,omitempty"`
	VolumeGb *uint32 `json:"volumeGb,omitempty"`
}

type storageListResponse struct {
	Items      []Storage      `json:"items"`
	Pagination PaginationInfo `json:"pagination"`
}

func (c *Client) CreateStorage(ctx context.Context, req CreateStorageRequest) (*Storage, error) {
	var out Storage
	if err := c.do(ctx, http.MethodPost, "/v1/storages", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetStorage(ctx context.Context, id string) (*Storage, error) {
	q := url.Values{"ids": {id}}
	var resp storageListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/storages", q, nil, &resp); err != nil {
		return nil, err
	}
	for i := range resp.Items {
		if resp.Items[i].ID == id {
			return &resp.Items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "storage not found"}
}

func (c *Client) UpdateStorage(ctx context.Context, id string, req UpdateStorageRequest) (*Storage, error) {
	var out Storage
	if err := c.do(ctx, http.MethodPatch, "/v1/storages/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteStorage(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/storages/delete", nil, idsBody{IDs: []string{id}}, nil)
}

// ---------- Public IPs ----------

type PublicIP struct {
	ID          string  `json:"id"`
	UserID      string  `json:"userId"`
	ClusterID   string  `json:"clusterId"`
	Name        string  `json:"name"`
	AddressType string  `json:"addressType"`
	Address     *string `json:"address,omitempty"`
	Status      string  `json:"status"`
	AttachedTo  *string `json:"attachedTo,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

type CreatePublicIPRequest struct {
	ClusterID   string `json:"clusterId"`
	Name        string `json:"name"`
	AddressType string `json:"addressType"`
}

type UpdatePublicIPRequest struct {
	Name *string `json:"name,omitempty"`
}

type publicIPListResponse struct {
	Items      []PublicIP     `json:"items"`
	Pagination PaginationInfo `json:"pagination"`
}

func (c *Client) CreatePublicIP(ctx context.Context, req CreatePublicIPRequest) (*PublicIP, error) {
	var out PublicIP
	if err := c.do(ctx, http.MethodPost, "/v1/public_ips", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetPublicIP(ctx context.Context, id string) (*PublicIP, error) {
	q := url.Values{"ids": {id}}
	var resp publicIPListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/public_ips", q, nil, &resp); err != nil {
		return nil, err
	}
	for i := range resp.Items {
		if resp.Items[i].ID == id {
			return &resp.Items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "public ip not found"}
}

func (c *Client) UpdatePublicIP(ctx context.Context, id string, req UpdatePublicIPRequest) (*PublicIP, error) {
	var out PublicIP
	if err := c.do(ctx, http.MethodPatch, "/v1/public_ips/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeletePublicIP(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/public_ips/delete", nil, idsBody{IDs: []string{id}}, nil)
}
