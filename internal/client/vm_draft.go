package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// /v3 draft lifecycle. A VM is created as a free draft with system defaults,
// refined through per-aspect mutations, and turned into a live VM by
// ProvisionVM. Everything after provision (read, rename, attach/detach data
// disks, restart, terminate) stays on /v2 — /v3 has no live-VM verbs.
// (graph @cloudless/fluence, node #1790)

// CreateVMDraft creates a draft VM from system defaults (POST /v3/vms, no body).
func (c *Client) CreateVMDraft(ctx context.Context) (*VM, error) {
	var out VM
	if err := c.do(ctx, http.MethodPost, "/v3/vms", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDraftVMRequest is the body of PATCH /v3/vms/{id}: name and SKU only.
type UpdateDraftVMRequest struct {
	Name            *string `json:"name,omitempty"`
	ConfigurationID *string `json:"configurationId,omitempty"`
}

func (c *Client) UpdateDraftVM(ctx context.Context, id string, req UpdateDraftVMRequest) (*VM, error) {
	var out VM
	if err := c.do(ctx, http.MethodPatch, "/v3/vms/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MoveDraftVMToCluster moves a draft and its draft sub-resources to another
// cluster (POST /v3/vms/{id}/cluster). expectedUpdatedAt is the draft's
// current updatedAt echoed verbatim: the server uses it as a revision fence.
func (c *Client) MoveDraftVMToCluster(ctx context.Context, id, clusterID, expectedUpdatedAt string) (*VM, error) {
	body := struct {
		ClusterID         string `json:"clusterId"`
		ExpectedUpdatedAt string `json:"expectedUpdatedAt"`
	}{ClusterID: clusterID, ExpectedUpdatedAt: expectedUpdatedAt}
	var out VM
	if err := c.do(ctx, http.MethodPost, "/v3/vms/"+id+"/cluster", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceDraftSSHKeys replaces the draft's SSH key set wholesale
// (PUT /v3/vms/{id}/ssh-keys).
func (c *Client) ReplaceDraftSSHKeys(ctx context.Context, id string, keyIDs []string) (*VM, error) {
	if keyIDs == nil {
		keyIDs = []string{}
	}
	body := struct {
		SSHKeys []string `json:"sshKeys"`
	}{SSHKeys: keyIDs}
	var out VM
	if err := c.do(ctx, http.MethodPut, "/v3/vms/"+id+"/ssh-keys", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DraftBootDisk is the untagged body of POST /v3/vms/{id}/boot-disk: either an
// existing Ready storage ID (serialized as a bare JSON string) or a create
// request for a new draft boot disk from a catalog image.
type DraftBootDisk struct {
	StorageID *string
	Create    *CreateDraftBootDisk
}

type CreateDraftBootDisk struct {
	VolumeGb uint32  `json:"volumeGb"`
	ImageID  string  `json:"imageId"`
	Name     *string `json:"name,omitempty"`
}

func (b DraftBootDisk) MarshalJSON() ([]byte, error) {
	if b.StorageID != nil {
		return json.Marshal(*b.StorageID)
	}
	return json.Marshal(b.Create)
}

// ReplaceDraftBootDisk replaces the draft's boot disk. A draft-created disk is
// hard-deleted by the server; a selected Ready disk survives.
func (c *Client) ReplaceDraftBootDisk(ctx context.Context, id string, disk DraftBootDisk) (*Storage, error) {
	var out Storage
	if err := c.do(ctx, http.MethodPost, "/v3/vms/"+id+"/boot-disk", nil, disk, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AttachDraftDataDisk attaches an existing data disk to a draft
// (POST /v3/vms/{id}/storages, AddDataDiskRequestBody: a bare storage-ID
// string selects the existing-disk variant).
func (c *Client) AttachDraftDataDisk(ctx context.Context, id, storageID string) (*Storage, error) {
	var out Storage
	if err := c.do(ctx, http.MethodPost, "/v3/vms/"+id+"/storages", nil, storageID, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProvisionVM turns a draft into a live VM (POST /v3/vms/{id}/provision). The
// server prices the draft graph, checks the balance and moves the VM and its
// draft sub-resources Draft -> New; readiness is then polled on /v2.
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
