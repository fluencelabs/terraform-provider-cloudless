# Cloudless Provider — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the new resource surface (security_group, storage, public_ip, vm updates, two attachment resources) and the cluster data source improvements from the spec at `docs/superpowers/specs/2026-05-03-cloudless-provider-full-surface-design.md`. Each new resource lands TDD-style with unit tests against an in-memory mock HTTP server.

**Architecture:** Three-layer Go module. `internal/client/` is plain HTTP/JSON over a typed Go API; `internal/client/mock/` is a real `httptest.Server` implementing the same surface in memory for fast unit tests; `internal/provider/` is the Terraform plugin-framework layer. Resources and tests live one-per-file alongside each other.

**Tech Stack:** Go 1.25, terraform-plugin-framework v1.19, terraform-plugin-framework-validators v0.19, terraform-plugin-testing (latest), `httptest` from std lib for the mock.

---

## Decomposition note

Phase 2 — validators package, generated docs (tfplugindocs), acceptance tests for all resources, CI workflow, goreleaser config — is **out of scope** for this plan and will be a separate plan written after Phase 1 ships. Phase 1 produces a building, unit-tested provider with the full Fluence resource surface.

## File structure

Files this plan creates or modifies:

```
.
├── go.mod                                                    # MODIFY: add terraform-plugin-testing
├── internal/
│   ├── client/
│   │   ├── client.go                                         # unchanged
│   │   ├── (split from client.go below — kept in one file for now)
│   │   └── mock/
│   │       └── server.go                                     # CREATE
│   └── provider/
│       ├── provider.go                                       # MODIFY: register new resources/data sources
│       ├── util.go                                           # MODIFY: clusterIDFromVPC helper
│       ├── ssh_key_resource.go                               # unchanged
│       ├── vpc_resource.go                                   # unchanged
│       ├── subnet_resource.go                                # MODIFY: cluster_id Optional + derive from VPC
│       ├── subnet_resource_test.go                           # CREATE
│       ├── vm_resource.go                                    # MODIFY: data_disk_ids list, drop public_ip block
│       ├── vm_resource_test.go                               # CREATE
│       ├── data_sources.go                                   # MODIFY: split + extend (see below)
│       ├── cluster_data_source.go                            # CREATE
│       ├── cluster_data_source_test.go                       # CREATE
│       ├── clusters_data_source.go                           # CREATE (split from data_sources.go, enhanced)
│       ├── clusters_data_source_test.go                      # CREATE
│       ├── security_group_resource.go                        # CREATE
│       ├── security_group_resource_test.go                   # CREATE
│       ├── storage_resource.go                               # CREATE
│       ├── storage_resource_test.go                          # CREATE
│       ├── public_ip_resource.go                             # CREATE
│       ├── public_ip_resource_test.go                        # CREATE
│       ├── vm_public_ip_attachment_resource.go               # CREATE
│       ├── vm_public_ip_attachment_resource_test.go          # CREATE
│       ├── security_group_attachment_resource.go             # CREATE
│       ├── security_group_attachment_resource_test.go        # CREATE
│       └── testing/
│           └── harness.go                                    # CREATE
└── examples/
    └── resources/
        ├── cloudless_security_group/resource.tf              # CREATE
        ├── cloudless_storage/resource.tf                     # CREATE
        ├── cloudless_public_ip/resource.tf                   # CREATE
        ├── cloudless_vm_public_ip_attachment/resource.tf     # CREATE
        └── cloudless_security_group_attachment/resource.tf   # CREATE
```

The existing `internal/client/client.go` is kept as a single file in this phase (it's still under 600 lines after additions). Splitting per-resource client files is a Phase 2 concern.

## How to read this plan

Each task is one logical unit (e.g., one resource end-to-end). Steps inside a task are 2-5 minute actions. **Do NOT skip steps.** TDD discipline: every step that adds behavior is preceded by a failing test.

When a step says "Run … Expected: …", run the command and verify the output matches. If it doesn't match, stop and figure out why — don't proceed assuming the test framework is broken.

**Run all Go commands inside `nix develop`** (the flake provides Go 1.25). Either `nix develop --command bash -c '...'` per command, or open a persistent shell with `nix develop`.

---

## Task 1: Set up the test harness and mock server skeleton

**Files:**
- Modify: `go.mod` (add `terraform-plugin-testing`)
- Create: `internal/client/mock/server.go`
- Create: `internal/provider/testing/harness.go`
- Create: `internal/client/mock/server_test.go` (smoke test)

The mock server starts empty and grows endpoint coverage as later tasks need it. This task establishes the scaffold and a smoke test proving the harness boots and serves a 404 cleanly.

- [ ] **Step 1.1: Add testing dep**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go get github.com/hashicorp/terraform-plugin-testing@latest && go mod tidy'
```
Expected: `go.mod` gains `github.com/hashicorp/terraform-plugin-testing` in the `require` block. No errors.

- [ ] **Step 1.2: Create the mock server skeleton**

Create `internal/client/mock/server.go`:

```go
// Package mock provides an in-memory implementation of the Fluence public API
// for use in unit tests. It exposes a *httptest.Server so tests can point a
// real *client.Client at it and exercise full HTTP/JSON paths.
package mock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// Server wraps an httptest.Server and holds the in-memory state of the
// resources it knows how to serve. Concrete endpoints are registered by the
// resource-specific tasks below; this file is the scaffold.
type Server struct {
	*httptest.Server
	mu sync.Mutex

	// State maps. Each resource type adds its own field as it lands.
	// Kept separate (rather than a generic map[string]any) so go vet is helpful.
}

// New starts a server and returns it. Callers must defer s.Close().
func New() *Server {
	s := &Server{}
	mux := http.NewServeMux()
	s.register(mux)
	s.Server = httptest.NewServer(mux)
	return s
}

// register wires all known endpoints. Future tasks add more handlers here.
func (s *Server) register(mux *http.ServeMux) {
	// Catch-all 404 with a JSON ErrorBody so client error decoding works.
	mux.HandleFunc("/", s.notFound)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "no route: " + r.Method + " " + r.URL.Path})
}

// writeJSON is a tiny helper used by every concrete handler.
func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError writes a Fluence ErrorBody-shaped JSON response.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 1.3: Create the smoke test**

Create `internal/client/mock/server_test.go`:

```go
package mock_test

import (
	"context"
	"testing"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

func TestMockServer_404IsTypedAPIError(t *testing.T) {
	srv := mock.New()
	defer srv.Close()

	c := client.New(srv.URL, "test-key")

	_, err := c.GetSSHKey(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !client.IsNotFound(err) {
		t.Fatalf("expected client.IsNotFound to be true, got %v", err)
	}
}
```

- [ ] **Step 1.4: Run the smoke test — expect FAIL first (route not registered)**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/client/mock/ -run TestMockServer_404IsTypedAPIError -v'
```
Expected: PASS. The catch-all `/` handler returns 404 with a JSON body, the client's `GetSSHKey` flows through `do()` which maps non-2xx + JSON `error` field into `*APIError{StatusCode: 404, Message: "no route: …"}`, and `IsNotFound` returns true.

If it FAILS instead, investigate — the routing or the client error decoding has drifted.

- [ ] **Step 1.5: Create the provider test harness**

Create `internal/provider/testing/harness.go`:

```go
// Package testing wires up a *resource.UnitTest-friendly provider that talks
// to an in-memory mock Fluence server.
package testing

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider"
)

// Harness bundles a mock server with a provider factory map ready to hand to
// resource.UnitTest.
type Harness struct {
	Mock      *mock.Server
	Client    *client.Client
	Factories map[string]func() (tfprotov6.ProviderServer, error)
}

// New starts a fresh mock + provider. Callers must defer h.Close().
func New() *Harness {
	srv := mock.New()
	c := client.New(srv.URL, "test-key")
	return &Harness{
		Mock:   srv,
		Client: c,
		Factories: map[string]func() (tfprotov6.ProviderServer, error){
			"cloudless": providerserver.NewProtocol6WithError(provider.NewWithClient(c)),
		},
	}
}

// Close shuts down the mock server.
func (h *Harness) Close() { h.Mock.Close() }

// Ctx returns a fresh background context for tests.
func (h *Harness) Ctx() context.Context { return context.Background() }
```

This requires `provider.NewWithClient` — a new constructor that bypasses the API-key path and uses a pre-built client. Add it next.

- [ ] **Step 1.6: Add `provider.NewWithClient`**

Modify `internal/provider/provider.go`. Add after the existing `New` function:

```go
// NewWithClient is used by unit tests to inject a pre-built client (typically
// pointed at a mock HTTP server). The returned provider skips the api_key
// resolution and uses the supplied client for every resource and data source.
func NewWithClient(c *client.Client) func() provider.Provider {
	return func() provider.Provider { return &cloudlessProvider{version: "test", overrideClient: c} }
}
```

And modify the `cloudlessProvider` struct at the top of the file:

```go
type cloudlessProvider struct {
	version        string
	overrideClient *client.Client // set by NewWithClient for tests
}
```

And modify `Configure`:

```go
func (p *cloudlessProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	if p.overrideClient != nil {
		resp.DataSourceData = p.overrideClient
		resp.ResourceData = p.overrideClient
		return
	}

	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := data.APIKey.ValueString()
	if apiKey == "" {
		apiKey = os.Getenv("FLUENCE_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			pathRoot("api_key"),
			"Missing Fluence API key",
			"Set the api_key provider attribute or the FLUENCE_API_KEY environment variable.",
		)
		return
	}

	endpoint := data.Endpoint.ValueString()
	if endpoint == "" {
		endpoint = os.Getenv("FLUENCE_ENDPOINT")
	}

	c := client.New(endpoint, apiKey, client.WithUserAgent("terraform-provider-cloudless/"+p.version))
	resp.DataSourceData = c
	resp.ResourceData = c
}
```

- [ ] **Step 1.7: Build everything to verify nothing broke**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./... && go vet ./...'
```
Expected: silent success.

- [ ] **Step 1.8: Re-run the smoke test**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./... -count=1'
```
Expected: `ok internal/client/mock` (the smoke test). Other packages should also pass (no test files yet means they'll show `[no test files]`, which is fine).

- [ ] **Step 1.9: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform
git add go.mod go.sum internal/client/mock/ internal/provider/testing/ internal/provider/provider.go
git commit -m "$(cat <<'EOF'
test: scaffold mock server + provider test harness

In-memory httptest server returning JSON ErrorBody on 404, plus a
test-only provider constructor that injects a pre-built client.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Subnet `cluster_id` becomes Optional with derive-from-VPC

**Files:**
- Modify: `internal/client/mock/server.go` (add minimal VPC GET handler so the resolver can fetch the parent VPC's cluster_id)
- Modify: `internal/provider/util.go` (add `resolveClusterID` helper)
- Modify: `internal/provider/subnet_resource.go` (cluster_id Optional + derive)
- Create: `internal/provider/subnet_resource_test.go`

The resolver lives in `util.go` so future resources that want to derive can reuse the contract. Validation: if both an explicit `cluster_id` and the parent's are present and differ → diagnostic error.

- [ ] **Step 2.1: Add VPC GET to the mock**

Append to `internal/client/mock/server.go`:

```go
// VPC state
type vpcRecord struct {
	ID, Name, ClusterID, UserID, Status string
}

func (s *Server) vpcs() *map[string]*vpcRecord {
	if s.vpcMap == nil {
		s.vpcMap = map[string]*vpcRecord{}
	}
	return &s.vpcMap
}
```

And add `vpcMap map[string]*vpcRecord` to the `Server` struct.

Add a handler for `GET /v1/vpcs?ids=<id>`:

```go
func (s *Server) handleVPCsGet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := r.URL.Query().Get("ids")
	items := []map[string]any{}
	for id, v := range s.vpcMap {
		if want != "" && id != want {
			continue
		}
		items = append(items, map[string]any{
			"id":           v.ID,
			"name":         v.Name,
			"clusterId":    v.ClusterID,
			"userId":       v.UserID,
			"status":       v.Status,
			"subnetsCount": 0,
			"createdAt":    "2026-01-01T00:00:00Z",
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
	})
}
```

Wire it in `register`:

```go
mux.HandleFunc("/v1/vpcs", func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
        s.handleVPCsGet(w, r)
        return
    }
    s.notFound(w, r)
})
```

Add a public test helper to seed VPCs:

```go
// SeedVPC inserts a VPC record. Tests use this to set up parent-VPC state
// for the subnet resolver.
func (s *Server) SeedVPC(id, name, clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vpcMap == nil {
		s.vpcMap = map[string]*vpcRecord{}
	}
	s.vpcMap[id] = &vpcRecord{ID: id, Name: name, ClusterID: clusterID, UserID: "test-user", Status: "ready"}
}
```

- [ ] **Step 2.2: Write the resolver helper test**

Create `internal/provider/util_test.go`:

```go
package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

func TestResolveClusterID_Explicit(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	c := client.New(srv.URL, "k")

	var diags diag.Diagnostics
	got := resolveClusterID(context.Background(), c, types.StringValue("cluster-A"), types.StringNull(), &diags)
	if got != "cluster-A" {
		t.Fatalf("got %q, want cluster-A", got)
	}
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
}

func TestResolveClusterID_DeriveFromVPC(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	srv.SeedVPC("vpc-1", "main", "cluster-X")
	c := client.New(srv.URL, "k")

	var diags diag.Diagnostics
	got := resolveClusterID(context.Background(), c, types.StringNull(), types.StringValue("vpc-1"), &diags)
	if got != "cluster-X" {
		t.Fatalf("got %q, want cluster-X", got)
	}
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
}

func TestResolveClusterID_MismatchErrors(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	srv.SeedVPC("vpc-1", "main", "cluster-X")
	c := client.New(srv.URL, "k")

	var diags diag.Diagnostics
	_ = resolveClusterID(context.Background(), c, types.StringValue("cluster-Y"), types.StringValue("vpc-1"), &diags)
	if !diags.HasError() {
		t.Fatal("expected mismatch error, got none")
	}
}

func TestResolveClusterID_NeitherErrors(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	c := client.New(srv.URL, "k")

	var diags diag.Diagnostics
	_ = resolveClusterID(context.Background(), c, types.StringNull(), types.StringNull(), &diags)
	if !diags.HasError() {
		t.Fatal("expected error when neither cluster_id nor vpc_id is set")
	}
}
```

- [ ] **Step 2.3: Run the resolver test — expect FAIL (function not defined)**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestResolveClusterID -v'
```
Expected: compilation error `undefined: resolveClusterID`. Good.

- [ ] **Step 2.4: Implement the resolver**

Append to `internal/provider/util.go`:

```go
// resolveClusterID returns the effective cluster_id for a resource.
// Order of preference: explicit, then derived from the supplied vpc_id.
// Errors via diags if both are unset or both are set and disagree.
func resolveClusterID(ctx context.Context, c *client.Client, explicit, vpcID types.String, diags *diag.Diagnostics) string {
	hasExplicit := !explicit.IsNull() && !explicit.IsUnknown() && explicit.ValueString() != ""
	hasVPC := !vpcID.IsNull() && !vpcID.IsUnknown() && vpcID.ValueString() != ""

	if !hasExplicit && !hasVPC {
		diags.AddError(
			"Missing cluster_id",
			"set cluster_id explicitly, or supply vpc_id so it can be derived from the parent VPC",
		)
		return ""
	}

	if hasExplicit && !hasVPC {
		return explicit.ValueString()
	}

	vpc, err := c.GetVPC(ctx, vpcID.ValueString())
	if err != nil {
		diags.AddError("Resolve cluster_id from vpc_id failed", err.Error())
		return ""
	}

	if hasExplicit && explicit.ValueString() != vpc.ClusterID {
		diags.AddError(
			"cluster_id / vpc_id mismatch",
			"the explicit cluster_id ("+explicit.ValueString()+") does not match the parent VPC's cluster_id ("+vpc.ClusterID+")",
		)
		return ""
	}

	return vpc.ClusterID
}
```

- [ ] **Step 2.5: Run resolver tests — expect PASS**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestResolveClusterID -v'
```
Expected: 4 tests PASS.

- [ ] **Step 2.6: Update subnet_resource.go schema and Create**

Modify `internal/provider/subnet_resource.go`:

In `Schema`, change `cluster_id` from Required to Optional + Computed:
```go
"cluster_id": schema.StringAttribute{
    Optional:      true,
    Computed:      true,
    PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
    Description:   "Cluster the subnet lives on. If unset, derived from vpc_id's cluster.",
},
```

In `Create`, before the `r.c.CreateSubnet` call, resolve:
```go
clusterID := resolveClusterID(ctx, r.c, plan.ClusterID, plan.VPCID, &resp.Diagnostics)
if resp.Diagnostics.HasError() {
    return
}
```

Then pass `clusterID` (not `plan.ClusterID.ValueString()`) into the request:
```go
out, err := r.c.CreateSubnet(ctx, plan.VPCID.ValueString(), client.CreateSubnetRequest{
    ClusterID: clusterID,
    // ...
})
```

- [ ] **Step 2.7: Write a resource-level integration test**

Create `internal/provider/subnet_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccSubnet_DerivesClusterIDFromVPC(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	h.Mock.SeedVPC("vpc-1", "main", "cluster-X")
	// (Subnet endpoints are auto-wired by mock.New(); no extra seeding needed.)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_subnet" "s" {
  vpc_id = "vpc-1"
  name   = "demo"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_subnet.s", "cluster_id", "cluster-X"),
				),
			},
		},
	})
}
```

- [ ] **Step 2.8: Add subnet endpoints to the mock**

Wire subnets into `New()` via `s.wireSubnetsOnce()` (add the call alongside the other wirings introduced by later tasks). Then:


Append to `internal/client/mock/server.go`:

```go
type subnetRecord struct {
	ID, Name, VPCID, ClusterID, UserID, Status string
	IPv4, IPv6                                 string
}

// wireSubnetsOnce is called from New() to register subnet handlers idempotently.
func (s *Server) wireSubnetsOnce() { s.subnetWiringOnce.Do(s.wireSubnets) }

func (s *Server) wireSubnets() {
	if s.subnetMap == nil {
		s.subnetMap = map[string]*subnetRecord{}
	}
	mux := s.muxFor("/v1/vpc/")
	mux.HandleFunc("/v1/vpc/", func(w http.ResponseWriter, r *http.Request) {
		// Path: /v1/vpc/{vpc_id}/subnets — create.
		if r.Method != http.MethodPost {
			s.notFound(w, r)
			return
		}
		// parse vpc_id from path
		// "/v1/vpc/<vpc>/subnets"
		parts := splitPath(r.URL.Path)
		if len(parts) != 4 || parts[0] != "v1" || parts[1] != "vpc" || parts[3] != "subnets" {
			s.notFound(w, r)
			return
		}
		vpcID := parts[2]
		var body struct {
			ClusterID, Name string
			IPv4Cidr        *string `json:"ipv4Cidr,omitempty"`
			IPv6Cidr        *string `json:"ipv6Cidr,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		id := newID()
		rec := &subnetRecord{ID: id, Name: body.Name, VPCID: vpcID, ClusterID: body.ClusterID, UserID: "test-user", Status: "ready"}
		if body.IPv4Cidr != nil {
			rec.IPv4 = *body.IPv4Cidr
		}
		if body.IPv6Cidr != nil {
			rec.IPv6 = *body.IPv6Cidr
		}
		s.subnetMap[id] = rec
		s.mu.Unlock()
		s.writeJSON(w, http.StatusOK, subnetWire(rec))
	})
	mux.HandleFunc("/v1/subnets", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			want := r.URL.Query().Get("ids")
			items := []map[string]any{}
			s.mu.Lock()
			for id, sn := range s.subnetMap {
				if want != "" && id != want {
					continue
				}
				items = append(items, subnetWire(sn))
			}
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, map[string]any{
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	mux.HandleFunc("/v1/subnets/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		for _, id := range body.IDs {
			delete(s.subnetMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

func subnetWire(rec *subnetRecord) map[string]any {
	out := map[string]any{
		"id":        rec.ID,
		"name":      rec.Name,
		"vpcId":     rec.VPCID,
		"clusterId": rec.ClusterID,
		"userId":    rec.UserID,
		"status":    rec.Status,
	}
	if rec.IPv4 != "" {
		out["ipv4Cidr"] = rec.IPv4
	}
	if rec.IPv6 != "" {
		out["ipv6Cidr"] = rec.IPv6
	}
	return out
}
```

You'll also need helpers `splitPath`, `newID`, `muxFor`, and a `subnetMap` field + `subnetWiringOnce sync.Once` in the `Server` struct. Add to `server.go`:

```go
import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// splitPath splits "/a/b/c" → ["a","b","c"].
func splitPath(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, v := range parts {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
}

// muxFor returns the underlying mux. We keep all handlers on a single mux for
// simplicity; this name is here to make the registration sites readable.
func (s *Server) muxFor(prefix string) *http.ServeMux {
	return s.mux
}
```

And restructure `New()` and `Server` to keep the mux as a field:

```go
type Server struct {
	*httptest.Server
	mu sync.Mutex
	mux *http.ServeMux

	vpcMap            map[string]*vpcRecord
	subnetMap         map[string]*subnetRecord
	subnetWiringOnce  sync.Once
}

func New() *Server {
	s := &Server{mux: http.NewServeMux()}
	s.register(s.mux)
	s.Server = httptest.NewServer(s.mux)
	return s
}
```

- [ ] **Step 2.9: Run the integration test — expect PASS**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccSubnet_DerivesClusterIDFromVPC -v'
```
Expected: PASS.

- [ ] **Step 2.10: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform
git add internal/client/mock/ internal/provider/util.go internal/provider/util_test.go internal/provider/subnet_resource.go internal/provider/subnet_resource_test.go
git commit -m "$(cat <<'EOF'
feat(subnet): cluster_id Optional, derives from vpc_id

resolveClusterID() helper picks explicit > vpc-derived; errors on
mismatch or both-unset. Mock server gains VPC + subnet endpoints.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Cluster data sources (singular `cloudless_cluster` + enhanced `cloudless_clusters`)

**Files:**
- Modify: `internal/client/client.go` (add `Datacenter` type + `ListDatacenters`; extend `Cluster` join)
- Modify: `internal/client/mock/server.go` (add `/v1/datacenters` handler + seeders)
- Modify: `internal/provider/data_sources.go` (remove the existing simple `cloudless_clusters`; replace via the new files)
- Create: `internal/provider/cluster_data_source.go`
- Create: `internal/provider/cluster_data_source_test.go`
- Create: `internal/provider/clusters_data_source.go`
- Create: `internal/provider/clusters_data_source_test.go`
- Modify: `internal/provider/provider.go` (register new data sources)

The two data sources share a denormalized `EnrichedCluster` shape (cluster fields + datacenter fields). Both transparently fetch + join.

- [ ] **Step 3.1: Add Datacenter type and ListDatacenters to client**

Append to `internal/client/client.go`:

```go
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
	Region            string   // = Datacenter.CountryCode
	CityCode          string
	DCSlug            string
	DCTier            int32
	DCCertifications  []string
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
```

- [ ] **Step 3.2: Add datacenters + clusters seeders to the mock**

Append to `internal/client/mock/server.go`:

```go
// SeedCluster adds a cluster (with a synthetic dc_id reference). Tests that
// also want country/city data should call SeedDatacenter() to register the
// referenced datacenter.
func (s *Server) SeedCluster(id, name, dcID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clusterMap == nil {
		s.clusterMap = map[string]map[string]any{}
	}
	s.clusterMap[id] = map[string]any{"id": id, "name": name, "dc_id": dcID}
	s.wireClustersOnce.Do(func() {
		s.mux.HandleFunc("/v1/clusters", func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			out := []map[string]any{}
			for _, c := range s.clusterMap {
				out = append(out, c)
			}
			s.writeJSON(w, http.StatusOK, out)
		})
	})
}

// SeedDatacenter registers a datacenter row.
func (s *Server) SeedDatacenter(id, country, city, slug string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dcMap == nil {
		s.dcMap = map[string]map[string]any{}
	}
	s.dcMap[id] = map[string]any{
		"id": id, "countryCode": country, "cityCode": city,
		"index": 0, "tier": 3, "certifications": []string{}, "slug": slug,
	}
	s.wireDCsOnce.Do(func() {
		s.mux.HandleFunc("/v1/datacenters", func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			out := []map[string]any{}
			for _, d := range s.dcMap {
				out = append(out, d)
			}
			s.writeJSON(w, http.StatusOK, out)
		})
	})
}
```

Add to `Server` struct: `clusterMap map[string]map[string]any`, `dcMap map[string]map[string]any`, `wireClustersOnce sync.Once`, `wireDCsOnce sync.Once`.

- [ ] **Step 3.3: Write the singular data source test (failing)**

Create `internal/provider/cluster_data_source_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccClusterDataSource_FilterByRegion(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	h.Mock.SeedDatacenter("dc-de", "DE", "FRA", "fra-1")
	h.Mock.SeedDatacenter("dc-pl", "PL", "WAW", "waw-1")
	h.Mock.SeedCluster("cluster-de", "Cloudless-DE", "dc-de")
	h.Mock.SeedCluster("cluster-pl", "Cloudless-PL", "dc-pl")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_cluster" "de" {
  region = "DE"
}
output "id"   { value = data.cloudless_cluster.de.id }
output "city" { value = data.cloudless_cluster.de.city_code }
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckOutput("id", "cluster-de"),
					resource.TestCheckOutput("city", "FRA"),
				),
			},
		},
	})
}

func TestAccClusterDataSource_AmbiguousErrors(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	h.Mock.SeedDatacenter("dc-de1", "DE", "FRA", "fra-1")
	h.Mock.SeedDatacenter("dc-de2", "DE", "BER", "ber-1")
	h.Mock.SeedCluster("cluster-fra", "Cloudless-FRA", "dc-de1")
	h.Mock.SeedCluster("cluster-ber", "Cloudless-BER", "dc-de2")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_cluster" "ambig" { region = "DE" }
`,
				ExpectError: regexpAmbiguous(),
			},
		},
	})
}

// regexpAmbiguous returns a compiled regex matching the diagnostic title used
// by cluster_data_source. Kept here so the message can evolve in one place.
func regexpAmbiguous() *regexp.Regexp { return regexp.MustCompile(`Ambiguous`) }
```

(Add `import "regexp"`.)

- [ ] **Step 3.4: Run — expect FAIL (data source unknown)**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccClusterDataSource -v'
```
Expected: error mentioning unknown `cloudless_cluster` data source.

- [ ] **Step 3.5: Implement the singular data source**

Create `internal/provider/cluster_data_source.go`:

```go
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewClusterDataSource() datasource.DataSource { return &clusterDS{} }

type clusterDS struct{ c *client.Client }

type clusterFilterModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Region   types.String `tfsdk:"region"`
	CityCode types.String `tfsdk:"city_code"`

	DCID             types.String   `tfsdk:"dc_id"`
	DCSlug           types.String   `tfsdk:"dc_slug"`
	DCTier           types.Int64    `tfsdk:"dc_tier"`
	DCCertifications []types.String `tfsdk:"dc_certifications"`
}

func (d *clusterDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (d *clusterDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up exactly one cluster by filter criteria. Errors if more than one matches.",
		Attributes: map[string]schema.Attribute{
			"id":        schema.StringAttribute{Optional: true, Computed: true, Description: "Explicit cluster UUID."},
			"name":      schema.StringAttribute{Optional: true, Computed: true},
			"region":    schema.StringAttribute{Optional: true, Computed: true, Description: "ISO 3166-1 alpha-2 country code (e.g. DE, PL)."},
			"city_code": schema.StringAttribute{Optional: true, Computed: true},

			"dc_id":             schema.StringAttribute{Computed: true},
			"dc_slug":           schema.StringAttribute{Computed: true},
			"dc_tier":           schema.Int64Attribute{Computed: true},
			"dc_certifications": schema.ListAttribute{ElementType: types.StringType, Computed: true},
		},
	}
}

func (d *clusterDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *clusterDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var filter clusterFilterModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &filter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.c.ListEnrichedClusters(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List clusters failed", err.Error())
		return
	}

	matches := []client.EnrichedCluster{}
	for _, c := range all {
		if v := filter.ID.ValueString(); v != "" && c.ID != v {
			continue
		}
		if v := filter.Name.ValueString(); v != "" && c.Name != v {
			continue
		}
		if v := filter.Region.ValueString(); v != "" && c.Region != v {
			continue
		}
		if v := filter.CityCode.ValueString(); v != "" && c.CityCode != v {
			continue
		}
		matches = append(matches, c)
	}

	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("No matching cluster", "no clusters matched the supplied filters")
		return
	case 1:
		// fall through
	default:
		names := []string{}
		for _, m := range matches {
			names = append(names, m.Name+"("+m.ID+")")
		}
		resp.Diagnostics.AddError(
			"Ambiguous cluster filter",
			"more than one cluster matches; narrow the filter. matches: "+joinComma(names),
		)
		return
	}

	c := matches[0]
	out := clusterFilterModel{
		ID:       types.StringValue(c.ID),
		Name:     types.StringValue(c.Name),
		Region:   types.StringValue(c.Region),
		CityCode: types.StringValue(c.CityCode),

		DCID:             types.StringValue(c.DCID),
		DCSlug:           types.StringValue(c.DCSlug),
		DCTier:           types.Int64Value(int64(c.DCTier)),
		DCCertifications: toStringList(c.DCCertifications),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// joinComma is a tiny helper to avoid importing strings just for this.
func joinComma(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
```

- [ ] **Step 3.6: Register the data source**

In `internal/provider/provider.go`, modify `DataSources`:

```go
func (p *cloudlessProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewClusterDataSource,           // NEW (singular)
		NewClustersDataSource,          // existing list, extended in step 3.8
		NewVMConfigurationsDataSource,
		NewDefaultImagesDataSource,
	}
}
```

- [ ] **Step 3.7: Run the singular tests — expect PASS**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccClusterDataSource -v'
```
Expected: 2 tests PASS.

- [ ] **Step 3.8: Extend `cloudless_clusters` with filters**

Move `clustersDS` from `data_sources.go` to a new file `internal/provider/clusters_data_source.go` and rewrite to support filters:

```go
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewClustersDataSource() datasource.DataSource { return &clustersDS{} }

type clustersDS struct{ c *client.Client }

type clustersListModel struct {
	Regions   []types.String `tfsdk:"regions"`
	CityCodes []types.String `tfsdk:"city_codes"`
	Names     []types.String `tfsdk:"names"`
	Clusters  []clusterModel `tfsdk:"clusters"`
}

type clusterModel struct {
	ID               types.String   `tfsdk:"id"`
	Name             types.String   `tfsdk:"name"`
	Region           types.String   `tfsdk:"region"`
	CityCode         types.String   `tfsdk:"city_code"`
	DCID             types.String   `tfsdk:"dc_id"`
	DCSlug           types.String   `tfsdk:"dc_slug"`
	DCTier           types.Int64    `tfsdk:"dc_tier"`
	DCCertifications []types.String `tfsdk:"dc_certifications"`
}

func (d *clustersDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_clusters"
}

func (d *clustersDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List clusters available to the calling account. Optionally filter by region (country) / city / name.",
		Attributes: map[string]schema.Attribute{
			"regions":    schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "ISO country codes; AND-composed with city_codes/names."},
			"city_codes": schema.ListAttribute{Optional: true, ElementType: types.StringType},
			"names":      schema.ListAttribute{Optional: true, ElementType: types.StringType},
			"clusters": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                schema.StringAttribute{Computed: true},
						"name":              schema.StringAttribute{Computed: true},
						"region":            schema.StringAttribute{Computed: true},
						"city_code":         schema.StringAttribute{Computed: true},
						"dc_id":             schema.StringAttribute{Computed: true},
						"dc_slug":           schema.StringAttribute{Computed: true},
						"dc_tier":           schema.Int64Attribute{Computed: true},
						"dc_certifications": schema.ListAttribute{ElementType: types.StringType, Computed: true},
					},
				},
			},
		},
	}
}

func (d *clustersDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *clustersDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var filter clustersListModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &filter)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.c.ListEnrichedClusters(ctx)
	if err != nil {
		resp.Diagnostics.AddError("List clusters failed", err.Error())
		return
	}

	regions := stringListSet(filter.Regions)
	cities := stringListSet(filter.CityCodes)
	names := stringListSet(filter.Names)

	out := clustersListModel{
		Regions:   filter.Regions,
		CityCodes: filter.CityCodes,
		Names:     filter.Names,
		Clusters:  []clusterModel{},
	}
	for _, c := range all {
		if !setMatch(regions, c.Region) {
			continue
		}
		if !setMatch(cities, c.CityCode) {
			continue
		}
		if !setMatch(names, c.Name) {
			continue
		}
		out.Clusters = append(out.Clusters, clusterModel{
			ID:               types.StringValue(c.ID),
			Name:             types.StringValue(c.Name),
			Region:           types.StringValue(c.Region),
			CityCode:         types.StringValue(c.CityCode),
			DCID:             types.StringValue(c.DCID),
			DCSlug:           types.StringValue(c.DCSlug),
			DCTier:           types.Int64Value(int64(c.DCTier)),
			DCCertifications: toStringList(c.DCCertifications),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// stringListSet builds a small lookup set; nil/empty input is "match all".
func stringListSet(in []types.String) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := map[string]struct{}{}
	for _, v := range in {
		if !v.IsNull() {
			out[v.ValueString()] = struct{}{}
		}
	}
	return out
}

func setMatch(set map[string]struct{}, v string) bool {
	if set == nil {
		return true
	}
	_, ok := set[v]
	return ok
}
```

- [ ] **Step 3.9: Delete the old simple `cloudless_clusters` from `data_sources.go`**

Open `internal/provider/data_sources.go`. Delete the block defining `clustersDS`, `clusterModel`, `clustersModel`, and `NewClustersDataSource` from that file (they're now in `clusters_data_source.go`). Keep the VM configurations and default images data sources where they are.

- [ ] **Step 3.10: Write the list-data-source test**

Create `internal/provider/clusters_data_source_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccClustersDataSource_FilterRegions(t *testing.T) {
	h := tfharness.New()
	defer h.Close()
	h.Mock.SeedDatacenter("dc-de", "DE", "FRA", "fra-1")
	h.Mock.SeedDatacenter("dc-pl", "PL", "WAW", "waw-1")
	h.Mock.SeedDatacenter("dc-us", "US", "JFK", "jfk-1")
	h.Mock.SeedCluster("c-de", "DE-1", "dc-de")
	h.Mock.SeedCluster("c-pl", "PL-1", "dc-pl")
	h.Mock.SeedCluster("c-us", "US-1", "dc-us")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
data "cloudless_clusters" "eu" {
  regions = ["DE", "PL"]
}
output "count" { value = length(data.cloudless_clusters.eu.clusters) }
`,
				Check: resource.TestCheckOutput("count", "2"),
			},
		},
	})
}
```

- [ ] **Step 3.11: Run all data source tests**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccCluster -v'
```
Expected: 3 tests PASS.

- [ ] **Step 3.12: Build all + commit**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./... && go vet ./...'
```
Expected: silent.

```bash
cd /home/ubuntu/projects/cloudless-terraform
git add internal/client/client.go internal/client/mock/ internal/provider/cluster_data_source.go internal/provider/cluster_data_source_test.go internal/provider/clusters_data_source.go internal/provider/clusters_data_source_test.go internal/provider/data_sources.go internal/provider/provider.go
git commit -m "$(cat <<'EOF'
feat(data): cloudless_cluster (singular) + filterable cloudless_clusters

Both data sources join /v1/clusters with /v1/datacenters, expose region
(country) / city_code / dc_* as computed fields. Singular errors on
ambiguity; plural supports regions/city_codes/names list filters (AND).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `cloudless_security_group` resource

**Files:**
- Modify: `internal/client/client.go` (add SG types + CRUD methods)
- Modify: `internal/client/mock/server.go` (SG endpoints + status transitions + seeders)
- Create: `internal/provider/security_group_resource.go`
- Create: `internal/provider/security_group_resource_test.go`
- Create: `examples/resources/cloudless_security_group/resource.tf`
- Modify: `internal/provider/provider.go` (register)

The shape: `ingress_mode` / `egress_mode` enums (`allow_all` / `allow_listed` / `deny_all`) plus repeated `ingress {}` / `egress {}` blocks. Validation: blocks must be empty unless mode is `allow_listed`.

- [ ] **Step 4.1: Add SG client types**

Append to `internal/client/client.go`:

```go
// ---------- Security groups ----------

type SecurityGroup struct {
	ID          string             `json:"id"`
	UserID      string             `json:"userId"`
	ClusterID   string             `json:"clusterId"`
	VPCID       string             `json:"vpcId"`
	Name        string             `json:"name"`
	Status      string             `json:"status"`
	IngressRules SecurityGroupRules `json:"ingressRules"`
	EgressRules  SecurityGroupRules `json:"egressRules"`
	AttachedTo  []string           `json:"attachedTo"`
	CreatedAt   string             `json:"createdAt"`
}

// SecurityGroupRules mirrors the API's discriminated union. We carry it as a
// struct with explicit Mode + Rules; helpers convert to/from wire JSON.
type SecurityGroupRules struct {
	// Mode is one of "allowAll", "allow" (with possibly empty Rules), or
	// "denyAll" (synthesized — wire form is allow + empty rules).
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
	Type         string         `json:"type"` // "ipv4" or "ipv6"
	ProtocolKind ProtocolKind   `json:"protocolKind"`
	Remote       SGRemote       `json:"remote"`
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
	// Try bare string first.
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		p.Kind = s
		return nil
	}
	// Then object form.
	var obj map[string]struct{ Ports Ports `json:"ports"` }
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	for k, v := range obj {
		p.Kind = k
		p.Ports = v.Ports
		return nil
	}
	return fmt.Errorf("ProtocolKind: empty object")
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
	return nil, fmt.Errorf("Ports: empty value")
}

func (p *Ports) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil && s == "all" {
		p.All = true
		return nil
	}
	var obj struct {
		Exact *struct{ Value uint16 `json:"value"` } `json:"exact"`
		Range *struct{ Min, Max uint16 `json:"min,omitempty"` }
	}
	// Custom decode for range to allow both keys.
	type rangePart struct{ Min, Max uint16 }
	var probe struct {
		Exact *struct{ Value uint16 `json:"value"` } `json:"exact"`
		Range *rangePart                              `json:"range"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		_ = obj
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
	return nil, fmt.Errorf("SGRemote: empty")
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

type CreateSecurityGroupRequest struct {
	ClusterID    string             `json:"clusterId"`
	Name         string             `json:"name"`
	IngressRules SecurityGroupRules `json:"ingressRules"`
	EgressRules  SecurityGroupRules `json:"egressRules"`
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

func (c *Client) UpdateSecurityGroup(ctx context.Context, id string, req UpdateSecurityGroupRequest) (*SecurityGroup, error) {
	var out SecurityGroup
	if err := c.do(ctx, http.MethodPatch, "/v1/security_groups/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteSecurityGroup(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/security_groups/delete", nil, idsBody{IDs: []string{id}}, nil)
}
```

- [ ] **Step 4.2: Add SG endpoints to mock**

Append to `internal/client/mock/server.go`:

```go
type sgRecord struct {
	ID, ClusterID, Name, UserID, Status, VPCID string
	Ingress, Egress                            json.RawMessage
}

func (s *Server) wireSGsOnce() { s.sgWiring.Do(s.wireSGs) }

func (s *Server) wireSGs() {
	if s.sgMap == nil {
		s.sgMap = map[string]*sgRecord{}
	}
	s.mux.HandleFunc("/v1/security_groups", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ClusterID    string          `json:"clusterId"`
				Name         string          `json:"name"`
				IngressRules json.RawMessage `json:"ingressRules"`
				EgressRules  json.RawMessage `json:"egressRules"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			id := newID()
			rec := &sgRecord{
				ID: id, ClusterID: body.ClusterID, Name: body.Name,
				UserID: "test-user", Status: "ready",
				Ingress: body.IngressRules, Egress: body.EgressRules,
			}
			s.sgMap[id] = rec
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, sgWire(rec))
		case http.MethodGet:
			want := r.URL.Query().Get("ids")
			items := []map[string]any{}
			s.mu.Lock()
			for id, sg := range s.sgMap {
				if want != "" && id != want {
					continue
				}
				items = append(items, sgWire(sg))
			}
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, map[string]any{
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	s.mux.HandleFunc("/v1/security_groups/", func(w http.ResponseWriter, r *http.Request) {
		// PATCH /v1/security_groups/{id}
		if r.Method != http.MethodPatch {
			s.notFound(w, r)
			return
		}
		parts := splitPath(r.URL.Path)
		if len(parts) != 3 {
			s.notFound(w, r)
			return
		}
		id := parts[2]
		var body struct {
			Name         *string         `json:"name"`
			IngressRules json.RawMessage `json:"ingressRules"`
			EgressRules  json.RawMessage `json:"egressRules"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.sgMap[id]
		if !ok {
			s.writeError(w, http.StatusNotFound, "security_group not found")
			return
		}
		if body.Name != nil {
			rec.Name = *body.Name
		}
		if len(body.IngressRules) > 0 {
			rec.Ingress = body.IngressRules
		}
		if len(body.EgressRules) > 0 {
			rec.Egress = body.EgressRules
		}
		s.writeJSON(w, http.StatusOK, sgWire(rec))
	})
	s.mux.HandleFunc("/v1/security_groups/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ IDs []string `json:"ids"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		for _, id := range body.IDs {
			delete(s.sgMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

func sgWire(rec *sgRecord) map[string]any {
	out := map[string]any{
		"id":         rec.ID,
		"clusterId":  rec.ClusterID,
		"name":       rec.Name,
		"userId":     rec.UserID,
		"status":     rec.Status,
		"vpcId":      rec.VPCID,
		"attachedTo": []string{},
		"createdAt":  "2026-01-01T00:00:00Z",
	}
	if len(rec.Ingress) > 0 {
		out["ingressRules"] = json.RawMessage(rec.Ingress)
	} else {
		out["ingressRules"] = map[string]string{"type": "allowAll"}
	}
	if len(rec.Egress) > 0 {
		out["egressRules"] = json.RawMessage(rec.Egress)
	} else {
		out["egressRules"] = map[string]string{"type": "allowAll"}
	}
	return out
}
```

Add to the `Server` struct: `sgMap map[string]*sgRecord` and `sgWiring sync.Once`. Call `s.wireSGsOnce()` in `New()`.

- [ ] **Step 4.3: Write the SG resource tests (FAIL first)**

Create `internal/provider/security_group_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccSecurityGroup_AllowAll(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_security_group" "wide" {
  cluster_id = "cluster-A"
  name       = "wide"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_security_group.wide", "ingress_mode", "allow_all"),
					resource.TestCheckResourceAttr("cloudless_security_group.wide", "egress_mode", "allow_all"),
					resource.TestCheckResourceAttr("cloudless_security_group.wide", "status", "ready"),
				),
			},
		},
	})
}

func TestAccSecurityGroup_AllowListed(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_security_group" "web" {
  cluster_id   = "cluster-A"
  name         = "web"
  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_security_group.web", "ingress.#", "1"),
			},
		},
	})
}

func TestAccSecurityGroup_DenyAll(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_security_group" "tight" {
  cluster_id  = "cluster-A"
  name        = "tight"
  egress_mode = "deny_all"
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_security_group.tight", "egress_mode", "deny_all"),
			},
		},
	})
}

func TestAccSecurityGroup_AllowListedRequiresBlocks(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_security_group" "broken" {
  cluster_id   = "cluster-A"
  name         = "broken"
  ingress_mode = "allow_listed"
  # No ingress blocks → should fail validation
}
`,
				ExpectError: regexpAtLeastOneRule(),
			},
		},
	})
}

func regexpAtLeastOneRule() *regexp.Regexp {
	return regexp.MustCompile(`at least one ingress block`)
}
```

(`regexp` import.)

- [ ] **Step 4.4: Run — expect FAIL (resource unknown)**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccSecurityGroup -v'
```
Expected: error mentioning unknown `cloudless_security_group` resource.

- [ ] **Step 4.5: Implement the SG resource**

Create `internal/provider/security_group_resource.go`:

```go
package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewSecurityGroupResource() resource.Resource { return &sgResource{} }

type sgResource struct{ c *client.Client }

type sgModel struct {
	ID          types.String `tfsdk:"id"`
	ClusterID   types.String `tfsdk:"cluster_id"`
	Name        types.String `tfsdk:"name"`
	IngressMode types.String `tfsdk:"ingress_mode"`
	EgressMode  types.String `tfsdk:"egress_mode"`
	Ingress     []sgRule     `tfsdk:"ingress"`
	Egress      []sgRule     `tfsdk:"egress"`
	Status      types.String `tfsdk:"status"`
	UserID      types.String `tfsdk:"user_id"`
	VPCID       types.String `tfsdk:"vpc_id"`
}

type sgRule struct {
	Protocol        types.String `tfsdk:"protocol"`
	Ports           types.String `tfsdk:"ports"`
	CIDR            types.String `tfsdk:"cidr"`
	SecurityGroupID types.String `tfsdk:"security_group_id"`
	Type            types.String `tfsdk:"type"`
}

func (r *sgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_group"
}

func sgRuleBlock() schema.NestedBlockObject {
	return schema.NestedBlockObject{
		Attributes: map[string]schema.Attribute{
			"protocol":          schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("tcp", "udp", "icmp", "all")}},
			"ports":             schema.StringAttribute{Optional: true},
			"cidr":              schema.StringAttribute{Optional: true},
			"security_group_id": schema.StringAttribute{Optional: true},
			"type":              schema.StringAttribute{Optional: true, Computed: true, Validators: []validator.String{stringvalidator.OneOf("ipv4", "ipv6")}},
		},
	}
}

func (r *sgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A security group on a Fluence cluster. Per-direction mode controls how rule blocks are interpreted.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"cluster_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{Required: true},
			"ingress_mode": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: `"allow_all" (default), "allow_listed" (requires ingress blocks), or "deny_all".`,
				Validators:  []validator.String{stringvalidator.OneOf("allow_all", "allow_listed", "deny_all")},
			},
			"egress_mode": schema.StringAttribute{
				Optional: true, Computed: true,
				Validators: []validator.String{stringvalidator.OneOf("allow_all", "allow_listed", "deny_all")},
			},
			"status":  schema.StringAttribute{Computed: true},
			"user_id": schema.StringAttribute{Computed: true},
			"vpc_id":  schema.StringAttribute{Computed: true},
		},
		Blocks: map[string]schema.Block{
			"ingress": schema.ListNestedBlock{NestedObject: sgRuleBlock()},
			"egress":  schema.ListNestedBlock{NestedObject: sgRuleBlock()},
		},
	}
}

func (r *sgResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// validateModeBlocks enforces: blocks present iff mode == allow_listed.
func validateModeBlocks(direction, mode string, blocks []sgRule, diags *path.Path) (string, error) {
	if mode == "" {
		mode = "allow_all"
	}
	switch mode {
	case "allow_all", "deny_all":
		if len(blocks) > 0 {
			return mode, fmt.Errorf("%s_mode = %q does not allow %s blocks (got %d)", direction, mode, direction, len(blocks))
		}
	case "allow_listed":
		if len(blocks) == 0 {
			return mode, fmt.Errorf("%s_mode = \"allow_listed\" requires at least one %s block", direction, direction)
		}
	default:
		return mode, fmt.Errorf("invalid %s_mode: %q", direction, mode)
	}
	return mode, nil
}

func (r *sgResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sgModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ingressMode, err := normalizeMode(plan.IngressMode)
	if err == nil {
		_, err = validateModeBlocks("ingress", ingressMode, plan.Ingress, nil)
	}
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("ingress_mode"), "Invalid ingress_mode", err.Error())
		return
	}
	egressMode, err := normalizeMode(plan.EgressMode)
	if err == nil {
		_, err = validateModeBlocks("egress", egressMode, plan.Egress, nil)
	}
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("egress_mode"), "Invalid egress_mode", err.Error())
		return
	}

	ingress, err := buildRules(ingressMode, plan.Ingress)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ingress rules", err.Error())
		return
	}
	egress, err := buildRules(egressMode, plan.Egress)
	if err != nil {
		resp.Diagnostics.AddError("Invalid egress rules", err.Error())
		return
	}

	out, err := r.c.CreateSecurityGroup(ctx, client.CreateSecurityGroupRequest{
		ClusterID:    plan.ClusterID.ValueString(),
		Name:         plan.Name.ValueString(),
		IngressRules: ingress,
		EgressRules:  egress,
	})
	if err != nil {
		resp.Diagnostics.AddError("Create security group failed", err.Error())
		return
	}

	id := out.ID
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetSecurityGroup(ctx, id)
		if err != nil {
			return err
		}
		out = got
		if isReady(got.Status) {
			return errStopPolling
		}
		if terminalFailure(got.Status) {
			return fmt.Errorf("security group %s entered terminal status %q", id, got.Status)
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for SG failed", err.Error())
		return
	}

	r.fill(&plan, out, ingressMode, egressMode)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sgResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sgModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetSecurityGroup(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read SG failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}
	ingressMode := apiToMode(out.IngressRules)
	egressMode := apiToMode(out.EgressRules)
	r.fill(&state, out, ingressMode, egressMode)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *sgResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sgModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ingressMode, _ := normalizeMode(plan.IngressMode)
	egressMode, _ := normalizeMode(plan.EgressMode)
	ingress, err := buildRules(ingressMode, plan.Ingress)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ingress", err.Error())
		return
	}
	egress, err := buildRules(egressMode, plan.Egress)
	if err != nil {
		resp.Diagnostics.AddError("Invalid egress", err.Error())
		return
	}

	upd := client.UpdateSecurityGroupRequest{
		IngressRules: &ingress,
		EgressRules:  &egress,
	}
	if !plan.Name.Equal(state.Name) {
		n := plan.Name.ValueString()
		upd.Name = &n
	}

	out, err := r.c.UpdateSecurityGroup(ctx, state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Update SG failed", err.Error())
		return
	}
	r.fill(&plan, out, ingressMode, egressMode)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sgResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sgModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.c.DeleteSecurityGroup(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete SG failed", err.Error())
		return
	}
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetSecurityGroup(ctx, id)
		if err != nil {
			if client.IsNotFound(err) {
				return errStopPolling
			}
			return err
		}
		if isRemoved(got.Status) {
			return errStopPolling
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for SG deletion failed", err.Error())
	}
}

func (r *sgResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *sgResource) fill(m *sgModel, sg *client.SecurityGroup, ingressMode, egressMode string) {
	m.ID = types.StringValue(sg.ID)
	m.ClusterID = types.StringValue(sg.ClusterID)
	m.Name = types.StringValue(sg.Name)
	m.IngressMode = types.StringValue(ingressMode)
	m.EgressMode = types.StringValue(egressMode)
	m.Status = types.StringValue(sg.Status)
	m.UserID = types.StringValue(sg.UserID)
	m.VPCID = types.StringValue(sg.VPCID)
	if ingressMode == "allow_listed" {
		m.Ingress = rulesToModel(sg.IngressRules.Rules)
	} else {
		m.Ingress = nil
	}
	if egressMode == "allow_listed" {
		m.Egress = rulesToModel(sg.EgressRules.Rules)
	} else {
		m.Egress = nil
	}
}

// normalizeMode returns the effective mode for an Optional+Computed
// types.String. Defaults unset to "allow_all".
func normalizeMode(s types.String) (string, error) {
	if s.IsNull() || s.IsUnknown() || s.ValueString() == "" {
		return "allow_all", nil
	}
	v := s.ValueString()
	switch v {
	case "allow_all", "allow_listed", "deny_all":
		return v, nil
	default:
		return "", fmt.Errorf("invalid mode: %q", v)
	}
}

// apiToMode classifies what we got back from the API.
func apiToMode(r client.SecurityGroupRules) string {
	switch r.Mode {
	case "allowAll":
		return "allow_all"
	case "allow":
		if len(r.Rules) == 0 {
			return "deny_all"
		}
		return "allow_listed"
	default:
		return "allow_all"
	}
}

// buildRules constructs the wire SecurityGroupRules for the given mode.
func buildRules(mode string, blocks []sgRule) (client.SecurityGroupRules, error) {
	switch mode {
	case "allow_all":
		return client.SecurityGroupRules{Mode: "allowAll"}, nil
	case "deny_all":
		return client.SecurityGroupRules{Mode: "allow", Rules: []client.SecurityGroupRule{}}, nil
	case "allow_listed":
		rules := make([]client.SecurityGroupRule, 0, len(blocks))
		for _, b := range blocks {
			rule, err := translateRule(b)
			if err != nil {
				return client.SecurityGroupRules{}, err
			}
			rules = append(rules, rule)
		}
		return client.SecurityGroupRules{Mode: "allow", Rules: rules}, nil
	default:
		return client.SecurityGroupRules{}, fmt.Errorf("invalid mode: %q", mode)
	}
}

func translateRule(b sgRule) (client.SecurityGroupRule, error) {
	r := client.SecurityGroupRule{Type: "ipv4"}
	if v := b.Type.ValueString(); v != "" {
		r.Type = v
	}

	pk := client.ProtocolKind{Kind: b.Protocol.ValueString()}
	switch pk.Kind {
	case "all", "icmp":
		if b.Ports.ValueString() != "" {
			return r, fmt.Errorf("ports must be empty for protocol %q", pk.Kind)
		}
	case "tcp", "udp":
		ports, err := parsePorts(b.Ports.ValueString())
		if err != nil {
			return r, err
		}
		pk.Ports = ports
	default:
		return r, fmt.Errorf("invalid protocol: %q", pk.Kind)
	}
	r.ProtocolKind = pk

	hasCIDR := b.CIDR.ValueString() != ""
	hasSG := b.SecurityGroupID.ValueString() != ""
	if hasCIDR == hasSG {
		return r, fmt.Errorf("exactly one of cidr / security_group_id is required per rule")
	}
	if hasCIDR {
		v := b.CIDR.ValueString()
		r.Remote.Address = &v
	}
	if hasSG {
		v := b.SecurityGroupID.ValueString()
		r.Remote.SecurityGroup = &v
	}
	return r, nil
}

func parsePorts(s string) (client.Ports, error) {
	if s == "" || s == "all" {
		return client.Ports{All: true}, nil
	}
	if !strings.Contains(s, "-") {
		v, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return client.Ports{}, fmt.Errorf("invalid port %q: %w", s, err)
		}
		p := uint16(v)
		return client.Ports{Exact: &p}, nil
	}
	parts := strings.SplitN(s, "-", 2)
	mn, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return client.Ports{}, fmt.Errorf("invalid range start %q: %w", parts[0], err)
	}
	mx, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return client.Ports{}, fmt.Errorf("invalid range end %q: %w", parts[1], err)
	}
	if mn > mx {
		return client.Ports{}, fmt.Errorf("invalid range %q (min > max)", s)
	}
	mn16, mx16 := uint16(mn), uint16(mx)
	return client.Ports{RangeMin: &mn16, RangeMax: &mx16}, nil
}

func rulesToModel(rules []client.SecurityGroupRule) []sgRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]sgRule, len(rules))
	for i, r := range rules {
		out[i] = sgRule{
			Type: types.StringValue(r.Type),
			Protocol: types.StringValue(r.ProtocolKind.Kind),
			Ports:    types.StringValue(portsToString(r.ProtocolKind)),
		}
		if r.Remote.Address != nil {
			out[i].CIDR = types.StringValue(*r.Remote.Address)
		} else {
			out[i].CIDR = types.StringNull()
		}
		if r.Remote.SecurityGroup != nil {
			out[i].SecurityGroupID = types.StringValue(*r.Remote.SecurityGroup)
		} else {
			out[i].SecurityGroupID = types.StringNull()
		}
	}
	return out
}

func portsToString(pk client.ProtocolKind) string {
	switch pk.Kind {
	case "all", "icmp":
		return ""
	case "tcp", "udp":
		if pk.Ports.All {
			return "all"
		}
		if pk.Ports.Exact != nil {
			return strconv.FormatUint(uint64(*pk.Ports.Exact), 10)
		}
		if pk.Ports.RangeMin != nil && pk.Ports.RangeMax != nil {
			return strconv.FormatUint(uint64(*pk.Ports.RangeMin), 10) + "-" + strconv.FormatUint(uint64(*pk.Ports.RangeMax), 10)
		}
	}
	return ""
}
```

- [ ] **Step 4.6: Register the resource**

In `internal/provider/provider.go`, modify `Resources`:

```go
func (p *cloudlessProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSSHKeyResource,
		NewVPCResource,
		NewSubnetResource,
		NewVMResource,
		NewSecurityGroupResource, // NEW
	}
}
```

- [ ] **Step 4.7: Add ConfigValidator for the mode-blocks rule**

The validator inside `Create`/`Update` returns user-readable diagnostics, but Terraform plan-time validation is stronger if implemented as a `ConfigValidators` method. Add to `sgResource`:

```go
func (r *sgResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		&modeBlocksValidator{direction: "ingress"},
		&modeBlocksValidator{direction: "egress"},
	}
}

type modeBlocksValidator struct{ direction string }

func (v *modeBlocksValidator) Description(_ context.Context) string {
	return "ensures " + v.direction + "_mode aligns with the presence of " + v.direction + " blocks"
}
func (v *modeBlocksValidator) MarkdownDescription(_ context.Context) string {
	return v.Description(context.Background())
}
func (v *modeBlocksValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m sgModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	mode := "allow_all"
	blocks := m.Ingress
	if v.direction == "egress" {
		blocks = m.Egress
		if !m.EgressMode.IsNull() && !m.EgressMode.IsUnknown() {
			mode = m.EgressMode.ValueString()
		}
	} else if !m.IngressMode.IsNull() && !m.IngressMode.IsUnknown() {
		mode = m.IngressMode.ValueString()
	}
	switch mode {
	case "allow_all", "deny_all":
		if len(blocks) > 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root(v.direction),
				"Blocks not allowed",
				fmt.Sprintf("%s_mode = %q does not allow %s blocks", v.direction, mode, v.direction),
			)
		}
	case "allow_listed":
		if len(blocks) == 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root(v.direction),
				"Missing rule blocks",
				fmt.Sprintf(`%s_mode = "allow_listed" requires at least one %s block`, v.direction, v.direction),
			)
		}
	}
}
```

- [ ] **Step 4.8: Run all SG tests — expect PASS**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccSecurityGroup -v'
```
Expected: 4 tests PASS.

- [ ] **Step 4.9: Add an example file**

Create `examples/resources/cloudless_security_group/resource.tf`:

```hcl
resource "cloudless_security_group" "web" {
  cluster_id = data.cloudless_cluster.main.id
  name       = "web"

  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
  ingress {
    protocol          = "tcp"
    ports             = "22"
    security_group_id = cloudless_security_group.bastion.id
  }

  # egress_mode defaults to allow_all
}
```

- [ ] **Step 4.10: Build, vet, commit**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./... && go vet ./...'
```
Expected: silent.

```bash
cd /home/ubuntu/projects/cloudless-terraform
git add internal/client/client.go internal/client/mock/server.go internal/provider/security_group_resource.go internal/provider/security_group_resource_test.go internal/provider/provider.go examples/resources/cloudless_security_group/
git commit -m "$(cat <<'EOF'
feat(security_group): new resource with mode enum + rule blocks

ingress_mode/egress_mode enums (allow_all default / allow_listed /
deny_all) replace inline-block ambiguity. ConfigValidators enforce
mode-vs-blocks consistency at plan time. Client uses discriminated
union types for protocolKind and remote.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `cloudless_storage` resource

**Files:**
- Modify: `internal/client/client.go` (Storage types + CRUD)
- Modify: `internal/client/mock/server.go` (storage endpoints + seeders)
- Create: `internal/provider/storage_resource.go`
- Create: `internal/provider/storage_resource_test.go`
- Create: `examples/resources/cloudless_storage/resource.tf`
- Modify: `internal/provider/provider.go` (register)

In-place updatable: `name`, `volume_gb`. ForceNew: `cluster_id`, `storage_type`, `replicated`, `os_image`.

- [ ] **Step 5.1: Add Storage client types**

Append to `internal/client/client.go`:

```go
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
```

- [ ] **Step 5.2: Add storage endpoints to mock**

Append to `internal/client/mock/server.go`:

```go
type storageRecord struct {
	ID, ClusterID, Name, StorageType, UserID, Status, Role string
	VolumeGb                                               uint64
	OSImage                                                string
}

func (s *Server) wireStoragesOnce() { s.storageWiring.Do(s.wireStorages) }

func (s *Server) wireStorages() {
	if s.storageMap == nil {
		s.storageMap = map[string]*storageRecord{}
	}
	s.mux.HandleFunc("/v1/storages", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ClusterID, Name, StorageType, OSImage string
				VolumeGb                              uint32
				Replicated                            bool
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			id := newID()
			role := "DATA"
			if body.OSImage != "" {
				role = "BOOT"
			}
			rec := &storageRecord{
				ID: id, ClusterID: body.ClusterID, Name: body.Name,
				StorageType: body.StorageType, UserID: "test-user", Status: "ready",
				Role: role, VolumeGb: uint64(body.VolumeGb), OSImage: body.OSImage,
			}
			s.storageMap[id] = rec
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, storageWire(rec))
		case http.MethodGet:
			want := r.URL.Query().Get("ids")
			items := []map[string]any{}
			s.mu.Lock()
			for id, st := range s.storageMap {
				if want != "" && id != want {
					continue
				}
				items = append(items, storageWire(st))
			}
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, map[string]any{
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	s.mux.HandleFunc("/v1/storages/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ IDs []string `json:"ids"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		for _, id := range body.IDs {
			delete(s.storageMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	s.mux.HandleFunc("/v1/storages/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			s.notFound(w, r)
			return
		}
		parts := splitPath(r.URL.Path)
		if len(parts) != 3 {
			s.notFound(w, r)
			return
		}
		id := parts[2]
		var body struct {
			Name     *string `json:"name"`
			VolumeGb *uint32 `json:"volumeGb"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.storageMap[id]
		if !ok {
			s.writeError(w, http.StatusNotFound, "storage not found")
			return
		}
		if body.Name != nil {
			rec.Name = *body.Name
		}
		if body.VolumeGb != nil {
			rec.VolumeGb = uint64(*body.VolumeGb)
		}
		s.writeJSON(w, http.StatusOK, storageWire(rec))
	})
}

func storageWire(rec *storageRecord) map[string]any {
	return map[string]any{
		"id":          rec.ID,
		"clusterId":   rec.ClusterID,
		"name":        rec.Name,
		"storageType": rec.StorageType,
		"userId":      rec.UserID,
		"status":      rec.Status,
		"role":        rec.Role,
		"volumeGb":    rec.VolumeGb,
		"attachedTo":  []string{},
		"createdAt":   "2026-01-01T00:00:00Z",
	}
}
```

Add `storageMap map[string]*storageRecord` and `storageWiring sync.Once` to `Server`. Call `s.wireStoragesOnce()` in `New()`.

- [ ] **Step 5.3: Write the failing test**

Create `internal/provider/storage_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccStorage_CreateAndResize(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "data" {
  cluster_id   = "cluster-A"
  name         = "data"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_storage.data", "volume_gb", "100"),
					resource.TestCheckResourceAttr("cloudless_storage.data", "status", "ready"),
					resource.TestCheckResourceAttr("cloudless_storage.data", "role", "DATA"),
				),
			},
			{
				Config: `
resource "cloudless_storage" "data" {
  cluster_id   = "cluster-A"
  name         = "data-renamed"
  storage_type = "NVME"
  volume_gb    = 200
  replicated   = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_storage.data", "name", "data-renamed"),
					resource.TestCheckResourceAttr("cloudless_storage.data", "volume_gb", "200"),
				),
			},
		},
	})
}
```

- [ ] **Step 5.4: Run — expect FAIL**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccStorage -v'
```
Expected: error mentioning unknown resource.

- [ ] **Step 5.5: Implement the resource**

Create `internal/provider/storage_resource.go`:

```go
package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewStorageResource() resource.Resource { return &storageResource{} }

type storageResource struct{ c *client.Client }

type storageModel struct {
	ID          types.String   `tfsdk:"id"`
	ClusterID   types.String   `tfsdk:"cluster_id"`
	Name        types.String   `tfsdk:"name"`
	StorageType types.String   `tfsdk:"storage_type"`
	VolumeGb    types.Int64    `tfsdk:"volume_gb"`
	Replicated  types.Bool     `tfsdk:"replicated"`
	OSImage     types.String   `tfsdk:"os_image"`
	Status      types.String   `tfsdk:"status"`
	Role        types.String   `tfsdk:"role"`
	UserID      types.String   `tfsdk:"user_id"`
	AttachedTo  []types.String `tfsdk:"attached_to"`
	CreatedAt   types.String   `tfsdk:"created_at"`
}

func (r *storageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage"
}

func (r *storageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A storage volume on a Fluence cluster.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"cluster_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name":       schema.StringAttribute{Required: true},
			"storage_type": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("NVME")},
			},
			"volume_gb":  schema.Int64Attribute{Required: true},
			"replicated": schema.BoolAttribute{Required: true},
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

func (r *storageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetStorage(ctx, id)
		if err != nil {
			return err
		}
		out = got
		if isReady(got.Status) {
			return errStopPolling
		}
		if terminalFailure(got.Status) {
			return fmt.Errorf("storage %s entered terminal status %q", id, got.Status)
		}
		return nil
	}); err != nil {
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
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		upd.Name = &v
	}
	if !plan.VolumeGb.Equal(state.VolumeGb) {
		v := uint32(plan.VolumeGb.ValueInt64())
		upd.VolumeGb = &v
	}
	out, err := r.c.UpdateStorage(ctx, state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Update storage failed", err.Error())
		return
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
	if err := r.c.DeleteStorage(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete storage failed", err.Error())
		return
	}
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetStorage(ctx, id)
		if err != nil {
			if client.IsNotFound(err) {
				return errStopPolling
			}
			return err
		}
		if isRemoved(got.Status) {
			return errStopPolling
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for storage deletion failed", err.Error())
	}
}

func (r *storageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
	m.AttachedTo = toStringList(s.AttachedTo)
	m.CreatedAt = types.StringValue(s.CreatedAt)
}
```

- [ ] **Step 5.6: Register the resource**

In `internal/provider/provider.go`, add `NewStorageResource` to the `Resources` slice.

- [ ] **Step 5.7: Run, expect PASS**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccStorage -v'
```
Expected: PASS.

- [ ] **Step 5.8: Add example**

Create `examples/resources/cloudless_storage/resource.tf`:

```hcl
resource "cloudless_storage" "data" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "data"
  storage_type = "NVME"
  volume_gb    = 200
  replicated   = false
}
```

- [ ] **Step 5.9: Build, vet, commit**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./... && go vet ./...'
cd /home/ubuntu/projects/cloudless-terraform
git add internal/client/client.go internal/client/mock/server.go internal/provider/storage_resource.go internal/provider/storage_resource_test.go internal/provider/provider.go examples/resources/cloudless_storage/
git commit -m "$(cat <<'EOF'
feat(storage): cloudless_storage resource

Cluster-scoped block storage volumes. In-place name + volume_gb
updates via PATCH; storage_type / replicated / os_image are ForceNew.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `cloudless_public_ip` resource

**Files:**
- Modify: `internal/client/client.go` (PublicIP types + CRUD)
- Modify: `internal/client/mock/server.go` (public IP endpoints)
- Create: `internal/provider/public_ip_resource.go`
- Create: `internal/provider/public_ip_resource_test.go`
- Create: `examples/resources/cloudless_public_ip/resource.tf`
- Modify: `internal/provider/provider.go`

Same lifecycle pattern as storage. PATCH covers `name` only.

- [ ] **Step 6.1: Add PublicIP client types**

Append to `internal/client/client.go`:

```go
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
```

- [ ] **Step 6.2: Add public IP endpoints to mock**

Append to `internal/client/mock/server.go`:

```go
type publicIPRecord struct {
	ID, ClusterID, Name, AddressType, UserID, Status string
	Address                                          string // synthesized
	AttachedTo                                       string
}

func (s *Server) wirePublicIPsOnce() { s.publicIPWiring.Do(s.wirePublicIPs) }

func (s *Server) wirePublicIPs() {
	if s.publicIPMap == nil {
		s.publicIPMap = map[string]*publicIPRecord{}
	}
	s.mux.HandleFunc("/v1/public_ips", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ClusterID, Name, AddressType string
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			id := newID()
			rec := &publicIPRecord{
				ID: id, ClusterID: body.ClusterID, Name: body.Name,
				AddressType: body.AddressType, UserID: "test-user", Status: "ready",
				Address: "203.0.113." + id[len(id)-2:],
			}
			s.publicIPMap[id] = rec
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, publicIPWire(rec))
		case http.MethodGet:
			want := r.URL.Query().Get("ids")
			items := []map[string]any{}
			s.mu.Lock()
			for id, p := range s.publicIPMap {
				if want != "" && id != want {
					continue
				}
				items = append(items, publicIPWire(p))
			}
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, map[string]any{
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	s.mux.HandleFunc("/v1/public_ips/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ IDs []string `json:"ids"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		for _, id := range body.IDs {
			delete(s.publicIPMap, id)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	s.mux.HandleFunc("/v1/public_ips/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			s.notFound(w, r)
			return
		}
		parts := splitPath(r.URL.Path)
		if len(parts) != 3 {
			s.notFound(w, r)
			return
		}
		id := parts[2]
		var body struct{ Name *string `json:"name"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.publicIPMap[id]
		if !ok {
			s.writeError(w, http.StatusNotFound, "public ip not found")
			return
		}
		if body.Name != nil {
			rec.Name = *body.Name
		}
		s.writeJSON(w, http.StatusOK, publicIPWire(rec))
	})
}

func publicIPWire(rec *publicIPRecord) map[string]any {
	out := map[string]any{
		"id":          rec.ID,
		"clusterId":   rec.ClusterID,
		"name":        rec.Name,
		"addressType": rec.AddressType,
		"userId":      rec.UserID,
		"status":      rec.Status,
		"createdAt":   "2026-01-01T00:00:00Z",
	}
	if rec.Address != "" {
		out["address"] = rec.Address
	}
	if rec.AttachedTo != "" {
		out["attachedTo"] = rec.AttachedTo
	}
	return out
}
```

Add `publicIPMap map[string]*publicIPRecord` and `publicIPWiring sync.Once` to `Server`. Call `s.wirePublicIPsOnce()` in `New()`.

- [ ] **Step 6.3: Write the failing test**

Create `internal/provider/public_ip_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccPublicIP_Create(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_public_ip" "edge" {
  cluster_id   = "cluster-A"
  name         = "edge"
  address_type = "V4"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_public_ip.edge", "address_type", "V4"),
					resource.TestCheckResourceAttr("cloudless_public_ip.edge", "status", "ready"),
					resource.TestCheckResourceAttrSet("cloudless_public_ip.edge", "address"),
				),
			},
		},
	})
}
```

- [ ] **Step 6.4: Run — expect FAIL**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccPublicIP -v'
```
Expected: error mentioning unknown resource.

- [ ] **Step 6.5: Implement the resource**

Create `internal/provider/public_ip_resource.go`:

```go
package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewPublicIPResource() resource.Resource { return &publicIPResource{} }

type publicIPResource struct{ c *client.Client }

type publicIPModel struct {
	ID          types.String `tfsdk:"id"`
	ClusterID   types.String `tfsdk:"cluster_id"`
	Name        types.String `tfsdk:"name"`
	AddressType types.String `tfsdk:"address_type"`
	Address     types.String `tfsdk:"address"`
	Status      types.String `tfsdk:"status"`
	AttachedTo  types.String `tfsdk:"attached_to"`
	UserID      types.String `tfsdk:"user_id"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *publicIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_ip"
}

func (r *publicIPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A public IP address that can be attached to a VM.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"cluster_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name":       schema.StringAttribute{Required: true},
			"address_type": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("V4")},
			},
			"address":     schema.StringAttribute{Computed: true, Description: "The actual IP string."},
			"status":      schema.StringAttribute{Computed: true},
			"attached_to": schema.StringAttribute{Computed: true},
			"user_id":     schema.StringAttribute{Computed: true},
			"created_at":  schema.StringAttribute{Computed: true},
		},
	}
}

func (r *publicIPResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *publicIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan publicIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.CreatePublicIP(ctx, client.CreatePublicIPRequest{
		ClusterID:   plan.ClusterID.ValueString(),
		Name:        plan.Name.ValueString(),
		AddressType: plan.AddressType.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Create public IP failed", err.Error())
		return
	}
	id := out.ID
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetPublicIP(ctx, id)
		if err != nil {
			return err
		}
		out = got
		if isReady(got.Status) {
			return errStopPolling
		}
		if terminalFailure(got.Status) {
			return fmt.Errorf("public ip %s entered terminal status %q", id, got.Status)
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for public IP failed", err.Error())
		return
	}
	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *publicIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state publicIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetPublicIP(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read public IP failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fill(&state, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *publicIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state publicIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		out, err := r.c.UpdatePublicIP(ctx, state.ID.ValueString(), client.UpdatePublicIPRequest{Name: &v})
		if err != nil {
			resp.Diagnostics.AddError("Update public IP failed", err.Error())
			return
		}
		r.fill(&plan, out)
	} else {
		out, err := r.c.GetPublicIP(ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Read public IP failed", err.Error())
			return
		}
		r.fill(&plan, out)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *publicIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state publicIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.c.DeletePublicIP(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete public IP failed", err.Error())
		return
	}
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetPublicIP(ctx, id)
		if err != nil {
			if client.IsNotFound(err) {
				return errStopPolling
			}
			return err
		}
		if isRemoved(got.Status) {
			return errStopPolling
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for public IP deletion failed", err.Error())
	}
}

func (r *publicIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *publicIPResource) fill(m *publicIPModel, p *client.PublicIP) {
	m.ID = types.StringValue(p.ID)
	m.ClusterID = types.StringValue(p.ClusterID)
	m.Name = types.StringValue(p.Name)
	m.AddressType = types.StringValue(p.AddressType)
	m.Address = stringFromPtr(p.Address)
	m.Status = types.StringValue(p.Status)
	m.AttachedTo = stringFromPtr(p.AttachedTo)
	m.UserID = types.StringValue(p.UserID)
	m.CreatedAt = types.StringValue(p.CreatedAt)
}
```

- [ ] **Step 6.6: Register the resource**

In `internal/provider/provider.go`, add `NewPublicIPResource` to `Resources`.

- [ ] **Step 6.7: Run, expect PASS**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccPublicIP -v'
```
Expected: PASS.

- [ ] **Step 6.8: Add example**

Create `examples/resources/cloudless_public_ip/resource.tf`:

```hcl
resource "cloudless_public_ip" "edge" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "edge"
  address_type = "V4"
}
```

- [ ] **Step 6.9: Build, vet, commit**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./... && go vet ./...'
cd /home/ubuntu/projects/cloudless-terraform
git add internal/client/client.go internal/client/mock/server.go internal/provider/public_ip_resource.go internal/provider/public_ip_resource_test.go internal/provider/provider.go examples/resources/cloudless_public_ip/
git commit -m "$(cat <<'EOF'
feat(public_ip): cloudless_public_ip resource

V4 IP allocation. PATCH for name only; address_type ForceNew.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Update `cloudless_vm` resource

**Files:**
- Modify: `internal/client/client.go` (extend VM types; add `AddVMStorages`, `RemoveVMStorages`, `AddVMPublicIP`, `RemoveVMPublicIP`, `ListVMInterfaces`, `UpdateVMInterface`)
- Modify: `internal/client/mock/server.go` (VM endpoints, attach endpoints, interface endpoints, storage transitions)
- Modify: `internal/provider/vm_resource.go` (drop `data_disks` block, drop `public_ip` block; add `data_disk_ids` list; add `network_interface_ids` computed)
- Create: `internal/provider/vm_resource_test.go`
- Update example `examples/main.tf` to match the new schema

The biggest moves: replace inline `data_disks {}` blocks with a `data_disk_ids = [...]` list (Required+Computed); implement smart Update that diffs the list and calls `/storages/add` / `/storages/remove`; expose `network_interface_ids` as a computed list.

- [ ] **Step 7.1: Extend VM client methods**

Modify `internal/client/client.go`. Replace the existing `CreateVMRequest` and supporting types with the simplified version, and add the attach/detach methods.

Replace the existing `VMBootDisk` definition with this (keeping the oneOf semantics for boot disk only — data disks become a flat ID list):

```go
// VMBootDisk is the boot-disk slot in CreateVMRequest. Either an existing
// storage ID (string) or an inline-create request.
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
	Name        string `json:"name"`
	StorageType string `json:"storageType"`
	VolumeGb    uint32 `json:"volumeGb"`
	Replicated  bool   `json:"replicated"`
	OSImage     string `json:"osImage,omitempty"`
}
```

Replace `CreateVMRequest`:

```go
type CreateVMRequest struct {
	ClusterID       string     `json:"clusterId"`
	Name            string     `json:"name"`
	ConfigurationID string     `json:"configurationId"`
	BootDisk        VMBootDisk `json:"bootDisk"`
	DataDisks       []string   `json:"dataDisks,omitempty"`
	SSHKeys         []string   `json:"sshKeys,omitempty"`
}
```

(Drop the `PublicIP` field — public IP attachment is now its own resource. Drop `VMPublicIPCreate` and `CreateUserPublicIPInline` if no longer referenced.)

Add the storage attach/detach methods:

```go
func (c *Client) AddVMStorages(ctx context.Context, vmID string, storageIDs []string) error {
	body := struct {
		DataDisks []string `json:"dataDisks"`
	}{DataDisks: storageIDs}
	return c.do(ctx, http.MethodPost, "/v2/vms/"+vmID+"/storages/add", nil, body, nil)
}

func (c *Client) RemoveVMStorages(ctx context.Context, vmID string, storageIDs []string) error {
	body := struct {
		DataDisks []string `json:"dataDisks"`
	}{DataDisks: storageIDs}
	return c.do(ctx, http.MethodPost, "/v2/vms/"+vmID+"/storages/remove", nil, body, nil)
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
	// nil pointer means clear; we send {"securityGroupId": null} explicitly.
	body := struct {
		SecurityGroupID *string `json:"securityGroupId"`
	}{SecurityGroupID: securityGroupID}
	return c.do(ctx, http.MethodPatch, "/v2/vms/"+vmID+"/interfaces/"+interfaceID, nil, body, nil)
}
```

- [ ] **Step 7.2: Extend the mock to support VMs + attach endpoints**

Append to `internal/client/mock/server.go`:

```go
type vmRecord struct {
	ID, ClusterID, ConfigurationID, Name, UserID, Status, BootDisk string
	DataDisks                                                       []string
	SSHKeys                                                         []string
	Subnets                                                         []string
	Interfaces                                                      []vmInterfaceRecord
	PublicIP                                                        string
	CreatedAt, UpdatedAt                                            string
}

type vmInterfaceRecord struct {
	ID              string
	SecurityGroupID *string
}

func (s *Server) wireVMsOnce() { s.vmWiring.Do(s.wireVMs) }

func (s *Server) wireVMs() {
	if s.vmMap == nil {
		s.vmMap = map[string]*vmRecord{}
	}
	s.mux.HandleFunc("/v2/vms", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ClusterID       string          `json:"clusterId"`
				ConfigurationID string          `json:"configurationId"`
				Name            string          `json:"name"`
				BootDisk        json.RawMessage `json:"bootDisk"`
				DataDisks       []string        `json:"dataDisks"`
				SSHKeys         []string        `json:"sshKeys"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			id := newID()
			rec := &vmRecord{
				ID: id, ClusterID: body.ClusterID, ConfigurationID: body.ConfigurationID,
				Name: body.Name, UserID: "test-user", Status: "launched",
				DataDisks: body.DataDisks, SSHKeys: body.SSHKeys,
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
			}
			// Decide bootDisk: if it parses as a JSON string, that's a storage ID.
			var asString string
			if json.Unmarshal(body.BootDisk, &asString) == nil {
				rec.BootDisk = asString
			} else {
				// Inline create: synthesize a storage ID.
				rec.BootDisk = newID()
			}
			// Auto-create one network interface.
			rec.Interfaces = []vmInterfaceRecord{{ID: newID()}}
			s.vmMap[id] = rec
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, vmWire(rec))
		case http.MethodGet:
			want := r.URL.Query().Get("ids")
			items := []map[string]any{}
			s.mu.Lock()
			for id, vm := range s.vmMap {
				if want != "" && id != want {
					continue
				}
				items = append(items, vmWire(vm))
			}
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, map[string]any{
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	s.mux.HandleFunc("/v2/vms/", func(w http.ResponseWriter, r *http.Request) {
		// Subpaths: /v2/vms/{id}, /v2/vms/{id}/storages/add, etc.
		parts := splitPath(r.URL.Path)
		if len(parts) < 3 {
			s.notFound(w, r)
			return
		}
		vmID := parts[2]
		s.mu.Lock()
		rec, ok := s.vmMap[vmID]
		s.mu.Unlock()
		if !ok {
			s.writeError(w, http.StatusNotFound, "vm not found")
			return
		}
		switch {
		case len(parts) == 3 && r.Method == http.MethodPatch:
			var body struct{ Name *string `json:"name"` }
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			if body.Name != nil {
				rec.Name = *body.Name
			}
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, vmWire(rec))
		case len(parts) == 4 && parts[3] == "terminate" && r.Method == http.MethodPost:
			s.mu.Lock()
			delete(s.vmMap, vmID)
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 5 && parts[3] == "storages" && parts[4] == "add" && r.Method == http.MethodPost:
			var body struct{ DataDisks []string `json:"dataDisks"` }
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			rec.DataDisks = append(rec.DataDisks, body.DataDisks...)
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 5 && parts[3] == "storages" && parts[4] == "remove" && r.Method == http.MethodPost:
			var body struct{ DataDisks []string `json:"dataDisks"` }
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			drop := map[string]bool{}
			for _, id := range body.DataDisks {
				drop[id] = true
			}
			kept := rec.DataDisks[:0]
			for _, id := range rec.DataDisks {
				if !drop[id] {
					kept = append(kept, id)
				}
			}
			rec.DataDisks = kept
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 5 && parts[3] == "public_ip" && parts[4] == "add" && r.Method == http.MethodPost:
			var body struct{ PublicIPID string `json:"publicIpId"` }
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			rec.PublicIP = body.PublicIPID
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 5 && parts[3] == "public_ip" && parts[4] == "remove" && r.Method == http.MethodPost:
			s.mu.Lock()
			rec.PublicIP = ""
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case len(parts) == 4 && parts[3] == "interfaces" && r.Method == http.MethodGet:
			s.mu.Lock()
			out := make([]map[string]any, 0, len(rec.Interfaces))
			for _, ni := range rec.Interfaces {
				m := map[string]any{"id": ni.ID}
				if ni.SecurityGroupID != nil {
					m["securityGroupId"] = *ni.SecurityGroupID
				}
				out = append(out, m)
			}
			s.mu.Unlock()
			s.writeJSON(w, http.StatusOK, out)
		case len(parts) == 5 && parts[3] == "interfaces" && r.Method == http.MethodPatch:
			interfaceID := parts[4]
			var body struct {
				SecurityGroupID *string `json:"securityGroupId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
			for i := range rec.Interfaces {
				if rec.Interfaces[i].ID == interfaceID {
					rec.Interfaces[i].SecurityGroupID = body.SecurityGroupID
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			s.writeError(w, http.StatusNotFound, "interface not found")
		default:
			s.notFound(w, r)
		}
	})
}

func vmWire(rec *vmRecord) map[string]any {
	out := map[string]any{
		"id":                rec.ID,
		"userId":            rec.UserID,
		"clusterId":         rec.ClusterID,
		"configurationId":   rec.ConfigurationID,
		"name":              rec.Name,
		"status":            rec.Status,
		"restartRequired":   false,
		"dataDisks":         rec.DataDisks,
		"subnets":           rec.Subnets,
		"sshKeys":           rec.SSHKeys,
		"createdAt":         rec.CreatedAt,
		"updatedAt":         rec.UpdatedAt,
	}
	ifaceIDs := make([]string, 0, len(rec.Interfaces))
	for _, ni := range rec.Interfaces {
		ifaceIDs = append(ifaceIDs, ni.ID)
	}
	out["networkInterfaces"] = ifaceIDs
	if rec.BootDisk != "" {
		out["bootDisk"] = rec.BootDisk
	}
	if rec.PublicIP != "" {
		out["publicIp"] = rec.PublicIP
	}
	return out
}
```

Add `vmMap map[string]*vmRecord` and `vmWiring sync.Once` to `Server`. Call `s.wireVMsOnce()` in `New()`.

- [ ] **Step 7.3: Write the failing VM test**

Create `internal/provider/vm_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccVM_CreateMinimal(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "cluster-A"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_vm" "app" {
  cluster_id       = "cluster-A"
  name             = "app"
  configuration_id = "cfg-1"

  boot_disk {
    storage_id = cloudless_storage.boot.id
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "status", "launched"),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface_ids.#", "1"),
				),
			},
		},
	})
}

func TestAccVM_DataDiskIDsSmartUpdate(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "cluster-A"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_storage" "data1" {
  cluster_id   = "cluster-A"
  name         = "data1"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_vm" "app" {
  cluster_id       = "cluster-A"
  name             = "app"
  configuration_id = "cfg-1"
  boot_disk { storage_id = cloudless_storage.boot.id }
  data_disk_ids    = [cloudless_storage.data1.id]
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "1"),
			},
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "cluster-A"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_storage" "data1" {
  cluster_id   = "cluster-A"
  name         = "data1"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_storage" "data2" {
  cluster_id   = "cluster-A"
  name         = "data2"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_vm" "app" {
  cluster_id       = "cluster-A"
  name             = "app"
  configuration_id = "cfg-1"
  boot_disk { storage_id = cloudless_storage.boot.id }
  data_disk_ids    = [cloudless_storage.data1.id, cloudless_storage.data2.id]
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "2"),
			},
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "cluster-A"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_storage" "data2" {
  cluster_id   = "cluster-A"
  name         = "data2"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_vm" "app" {
  cluster_id       = "cluster-A"
  name             = "app"
  configuration_id = "cfg-1"
  boot_disk { storage_id = cloudless_storage.boot.id }
  data_disk_ids    = [cloudless_storage.data2.id]
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "1"),
			},
		},
	})
}
```

- [ ] **Step 7.4: Run — expect FAIL (schema is the old one with data_disks block)**

Run:
```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccVM -v'
```
Expected: FAIL (either "Unsupported argument: data_disk_ids" or similar schema mismatch).

- [ ] **Step 7.5: Rewrite the VM resource**

Replace the contents of `internal/provider/vm_resource.go` with:

```go
package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

func NewVMResource() resource.Resource { return &vmResource{} }

type vmResource struct{ c *client.Client }

type vmModel struct {
	ID              types.String `tfsdk:"id"`
	ClusterID       types.String `tfsdk:"cluster_id"`
	Name            types.String `tfsdk:"name"`
	ConfigurationID types.String `tfsdk:"configuration_id"`

	BootDisk    *vmBootDiskModel `tfsdk:"boot_disk"`
	DataDiskIDs []types.String   `tfsdk:"data_disk_ids"`
	SSHKeyIDs   []types.String   `tfsdk:"ssh_key_ids"`

	Status              types.String   `tfsdk:"status"`
	UserID              types.String   `tfsdk:"user_id"`
	BootDiskID          types.String   `tfsdk:"boot_disk_id"`
	NetworkInterfaceIDs []types.String `tfsdk:"network_interface_ids"`
	PublicIPID          types.String   `tfsdk:"public_ip_id"`
	RestartRequired     types.Bool     `tfsdk:"restart_required"`
	CreatedAt           types.String   `tfsdk:"created_at"`
	UpdatedAt           types.String   `tfsdk:"updated_at"`
}

type vmBootDiskModel struct {
	StorageID   types.String `tfsdk:"storage_id"`
	Name        types.String `tfsdk:"name"`
	StorageType types.String `tfsdk:"storage_type"`
	VolumeGb    types.Int64  `tfsdk:"volume_gb"`
	Replicated  types.Bool   `tfsdk:"replicated"`
	OSImage     types.String `tfsdk:"os_image"`
}

func (r *vmResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm"
}

func (r *vmResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A virtual machine on a Fluence cluster.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"cluster_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name":       schema.StringAttribute{Required: true},
			"configuration_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"data_disk_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: "Storage IDs to attach as data disks. Smart-update: changes diff old vs new and call /storages/add and /storages/remove.",
			},
			"ssh_key_ids": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				PlanModifiers: []planmodifier.List{ /* ForceNew: see ConfigValidators below */ },
			},
			"status":                schema.StringAttribute{Computed: true},
			"user_id":               schema.StringAttribute{Computed: true},
			"boot_disk_id":          schema.StringAttribute{Computed: true},
			"network_interface_ids": schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"public_ip_id":          schema.StringAttribute{Computed: true, Description: "Read-only mirror of any cloudless_vm_public_ip_attachment binding."},
			"restart_required":      schema.BoolAttribute{Computed: true},
			"created_at":            schema.StringAttribute{Computed: true},
			"updated_at":            schema.StringAttribute{Computed: true},
		},
		Blocks: map[string]schema.Block{
			"boot_disk": schema.SingleNestedBlock{
				Description: "Boot disk: either reference an existing storage_id or supply name/storage_type/volume_gb/replicated/os_image to inline-create.",
				Attributes: map[string]schema.Attribute{
					"storage_id":   schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()}},
					"name":         schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()}},
					"storage_type": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("NVME")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()}},
					"volume_gb":    schema.Int64Attribute{Optional: true},
					"replicated":   schema.BoolAttribute{Optional: true},
					"os_image":     schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()}},
				},
			},
		},
	}
}

func (r *vmResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func vmBootDiskToAPI(d *vmBootDiskModel) (client.VMBootDisk, error) {
	if d == nil {
		return client.VMBootDisk{}, fmt.Errorf("boot_disk is required")
	}
	if v := d.StorageID.ValueString(); v != "" {
		return client.VMBootDisk{StorageID: &v}, nil
	}
	if d.Name.IsNull() || d.StorageType.IsNull() || d.VolumeGb.IsNull() || d.Replicated.IsNull() {
		return client.VMBootDisk{}, fmt.Errorf("inline boot_disk requires name, storage_type, volume_gb, and replicated")
	}
	return client.VMBootDisk{Create: &client.CreateUserStorageInline{
		Name:        d.Name.ValueString(),
		StorageType: d.StorageType.ValueString(),
		VolumeGb:    uint32(d.VolumeGb.ValueInt64()),
		Replicated:  d.Replicated.ValueBool(),
		OSImage:     d.OSImage.ValueString(),
	}}, nil
}

func (r *vmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bd, err := vmBootDiskToAPI(plan.BootDisk)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("boot_disk"), "Invalid boot_disk", err.Error())
		return
	}

	dataIDs := stringSliceFromList(plan.DataDiskIDs)
	sshKeys := stringSliceFromList(plan.SSHKeyIDs)

	out, err := r.c.CreateVM(ctx, client.CreateVMRequest{
		ClusterID:       plan.ClusterID.ValueString(),
		Name:            plan.Name.ValueString(),
		ConfigurationID: plan.ConfigurationID.ValueString(),
		BootDisk:        bd,
		DataDisks:       dataIDs,
		SSHKeys:         sshKeys,
	})
	if err != nil {
		resp.Diagnostics.AddError("Create VM failed", err.Error())
		return
	}

	id := out.ID
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetVM(ctx, id)
		if err != nil {
			return err
		}
		out = got
		if isReady(got.Status) {
			return errStopPolling
		}
		if terminalFailure(got.Status) {
			return fmt.Errorf("vm %s entered terminal status %q", id, got.Status)
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for VM failed", err.Error())
		return
	}

	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.GetVM(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read VM failed", err.Error())
		return
	}
	if isRemoved(out.Status) {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fill(&state, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		if _, err := r.c.UpdateVM(ctx, state.ID.ValueString(), client.UpdateVMRequest{Name: &v}); err != nil {
			resp.Diagnostics.AddError("Rename VM failed", err.Error())
			return
		}
	}

	// Smart-update data disks: diff old vs new.
	oldSet := stringSetFromList(state.DataDiskIDs)
	newSet := stringSetFromList(plan.DataDiskIDs)
	toAdd := []string{}
	for id := range newSet {
		if !oldSet[id] {
			toAdd = append(toAdd, id)
		}
	}
	toRemove := []string{}
	for id := range oldSet {
		if !newSet[id] {
			toRemove = append(toRemove, id)
		}
	}
	if len(toAdd) > 0 {
		if err := r.c.AddVMStorages(ctx, state.ID.ValueString(), toAdd); err != nil {
			resp.Diagnostics.AddError("Attach storages failed", err.Error())
			return
		}
	}
	if len(toRemove) > 0 {
		if err := r.c.RemoveVMStorages(ctx, state.ID.ValueString(), toRemove); err != nil {
			resp.Diagnostics.AddError("Detach storages failed", err.Error())
			return
		}
	}

	out, err := r.c.GetVM(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Re-read VM failed", err.Error())
		return
	}
	r.fill(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.c.TerminateVM(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Terminate VM failed", err.Error())
		return
	}
	if err := waitFor(ctx, defaultPoll(), func(ctx context.Context) error {
		got, err := r.c.GetVM(ctx, id)
		if err != nil {
			if client.IsNotFound(err) {
				return errStopPolling
			}
			return err
		}
		if isRemoved(got.Status) {
			return errStopPolling
		}
		return nil
	}); err != nil {
		resp.Diagnostics.AddError("Waiting for VM termination failed", err.Error())
	}
}

func (r *vmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *vmResource) fill(m *vmModel, v *client.VM) {
	m.ID = types.StringValue(v.ID)
	m.ClusterID = types.StringValue(v.ClusterID)
	m.ConfigurationID = types.StringValue(v.ConfigurationID)
	m.Name = types.StringValue(v.Name)
	m.Status = types.StringValue(v.Status)
	m.UserID = types.StringValue(v.UserID)
	m.BootDiskID = stringFromPtr(v.BootDisk)
	m.DataDiskIDs = toStringList(v.DataDisks)
	m.NetworkInterfaceIDs = toStringList(v.NetworkInterfaces)
	m.PublicIPID = stringFromPtr(v.PublicIP)
	m.RestartRequired = types.BoolValue(v.RestartRequired)
	m.CreatedAt = types.StringValue(v.CreatedAt)
	m.UpdatedAt = types.StringValue(v.UpdatedAt)
}

// stringSliceFromList flattens a []types.String into []string, dropping nulls.
func stringSliceFromList(in []types.String) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !s.IsNull() && !s.IsUnknown() {
			out = append(out, s.ValueString())
		}
	}
	return out
}

func stringSetFromList(in []types.String) map[string]bool {
	out := map[string]bool{}
	for _, s := range in {
		if !s.IsNull() && !s.IsUnknown() {
			out[s.ValueString()] = true
		}
	}
	return out
}
```

- [ ] **Step 7.6: Run VM tests, expect PASS**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccVM -v'
```
Expected: 2 tests PASS.

- [ ] **Step 7.7: Update examples/main.tf to match the new schema**

Edit `examples/main.tf`. Replace the existing `cloudless_vm "example"` block with one that uses `data_disk_ids` and a separate `cloudless_storage` for the boot disk:

```hcl
resource "cloudless_storage" "boot" {
  cluster_id   = local.cluster_id
  name         = "example-boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = local.ubuntu_image
}

resource "cloudless_vm" "example" {
  cluster_id       = local.cluster_id
  name             = "example-vm"
  configuration_id = local.small_config_id
  ssh_key_ids      = [cloudless_ssh_key.me.id]

  boot_disk {
    storage_id = cloudless_storage.boot.id
  }

  depends_on = [cloudless_subnet.default]
}
```

(Remove the inline `boot_disk { name = ... }` and `public_ip {}` blocks from the old example.)

- [ ] **Step 7.8: Build, vet, full test, commit**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./... && go vet ./... && go test ./...'
cd /home/ubuntu/projects/cloudless-terraform
git add internal/client/client.go internal/client/mock/server.go internal/provider/vm_resource.go internal/provider/vm_resource_test.go examples/main.tf
git commit -m "$(cat <<'EOF'
feat(vm): data_disk_ids list with smart Update; drop inline public_ip block

CreateVM payload now passes dataDisks as a flat ID list (oneOf=string).
Update diffs old vs new and calls /storages/add and /storages/remove.
Inline data_disks{} and public_ip{} blocks removed; public IP is a
separate cloudless_vm_public_ip_attachment resource (next task).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: `cloudless_vm_public_ip_attachment` resource

**Files:**
- Create: `internal/provider/vm_public_ip_attachment_resource.go`
- Create: `internal/provider/vm_public_ip_attachment_resource_test.go`
- Create: `examples/resources/cloudless_vm_public_ip_attachment/resource.tf`
- Modify: `internal/provider/provider.go`

The mock already has the endpoints (added in Task 7.2). Client methods too (Task 7.1). Just need the resource layer.

- [ ] **Step 8.1: Write the failing test**

Create `internal/provider/vm_public_ip_attachment_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccVMPublicIPAttachment_AttachAndDetach(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "cluster-A"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_vm" "app" {
  cluster_id       = "cluster-A"
  name             = "app"
  configuration_id = "cfg-1"
  boot_disk { storage_id = cloudless_storage.boot.id }
}

resource "cloudless_public_ip" "edge" {
  cluster_id   = "cluster-A"
  name         = "edge"
  address_type = "V4"
}

resource "cloudless_vm_public_ip_attachment" "att" {
  vm_id        = cloudless_vm.app.id
  public_ip_id = cloudless_public_ip.edge.id
}
`,
				Check: resource.TestCheckResourceAttrPair(
					"cloudless_vm_public_ip_attachment.att", "vm_id",
					"cloudless_vm.app", "id",
				),
			},
		},
	})
}
```

- [ ] **Step 8.2: Run, expect FAIL**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccVMPublicIPAttachment -v'
```
Expected: error mentioning unknown resource.

- [ ] **Step 8.3: Implement**

Create `internal/provider/vm_public_ip_attachment_resource.go`:

```go
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

func NewVMPublicIPAttachmentResource() resource.Resource { return &vmPublicIPAttachmentResource{} }

type vmPublicIPAttachmentResource struct{ c *client.Client }

type vmPublicIPAttachmentModel struct {
	ID         types.String `tfsdk:"id"`
	VMID       types.String `tfsdk:"vm_id"`
	PublicIPID types.String `tfsdk:"public_ip_id"`
}

func (r *vmPublicIPAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_public_ip_attachment"
}

func (r *vmPublicIPAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Attach a cloudless_public_ip to a cloudless_vm. Both attributes are ForceNew.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"vm_id":        schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"public_ip_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		},
	}
}

func (r *vmPublicIPAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *vmPublicIPAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmPublicIPAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.AddVMPublicIP(ctx, plan.VMID.ValueString(), plan.PublicIPID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Attach public IP failed", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.VMID.ValueString() + ":" + plan.PublicIPID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmPublicIPAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmPublicIPAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err := r.c.GetVM(ctx, state.VMID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read VM failed", err.Error())
		return
	}
	if vm.PublicIP == nil || *vm.PublicIP != state.PublicIPID.ValueString() {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmPublicIPAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Both attributes are RequiresReplace, so Update should never be called.
	// If it is (e.g., a future schema field is added), preserve state.
	var plan vmPublicIPAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmPublicIPAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmPublicIPAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// API: /public_ip/remove takes no body and removes whatever IP is bound.
	// Fetch current state first so we don't accidentally remove a different IP
	// the user attached out-of-band.
	vm, err := r.c.GetVM(ctx, state.VMID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Read VM during attachment delete failed", err.Error())
		return
	}
	if vm.PublicIP == nil || *vm.PublicIP != state.PublicIPID.ValueString() {
		// Already detached or replaced — nothing to do.
		return
	}
	if err := r.c.RemoveVMPublicIP(ctx, state.VMID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Detach public IP failed", err.Error())
	}
}

func (r *vmPublicIPAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, ":", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID", "expected <vm_id>:<public_ip_id>")
		return
	}
	resp.State.SetAttribute(ctx, path.Root("id"), req.ID)
	resp.State.SetAttribute(ctx, path.Root("vm_id"), parts[0])
	resp.State.SetAttribute(ctx, path.Root("public_ip_id"), parts[1])
}
```

- [ ] **Step 8.4: Register**

In `internal/provider/provider.go`, add `NewVMPublicIPAttachmentResource` to `Resources`.

- [ ] **Step 8.5: Run, expect PASS**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccVMPublicIPAttachment -v'
```
Expected: PASS.

- [ ] **Step 8.6: Add example**

Create `examples/resources/cloudless_vm_public_ip_attachment/resource.tf`:

```hcl
resource "cloudless_public_ip" "edge" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "edge"
  address_type = "V4"
}

resource "cloudless_vm_public_ip_attachment" "edge" {
  vm_id        = cloudless_vm.app.id
  public_ip_id = cloudless_public_ip.edge.id
}
```

- [ ] **Step 8.7: Build, vet, commit**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./... && go vet ./...'
cd /home/ubuntu/projects/cloudless-terraform
git add internal/provider/vm_public_ip_attachment_resource.go internal/provider/vm_public_ip_attachment_resource_test.go internal/provider/provider.go examples/resources/cloudless_vm_public_ip_attachment/
git commit -m "$(cat <<'EOF'
feat(public_ip_attachment): cloudless_vm_public_ip_attachment

Wraps /v2/vms/{id}/public_ip/{add,remove}. Both attributes ForceNew.
Delete checks current state first to avoid removing an out-of-band IP.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `cloudless_security_group_attachment` resource

**Files:**
- Create: `internal/provider/security_group_attachment_resource.go`
- Create: `internal/provider/security_group_attachment_resource_test.go`
- Create: `examples/resources/cloudless_security_group_attachment/resource.tf`
- Modify: `internal/provider/provider.go`
- Modify: `internal/client/client.go` (small helper to find vm_id by interface_id)

The PATCH endpoint requires `vm_id`, but our resource keys on `network_interface_id` (interface UUIDs are globally unique). We resolve `vm_id` at create time by listing VMs and finding the one that owns the interface; store it as a computed attribute for subsequent PATCH/DELETE calls.

- [ ] **Step 9.1: Add the resolver helper to the client**

Append to `internal/client/client.go`:

```go
// FindVMByInterface lists VMs and returns the one whose networkInterfaces
// contains the given interface_id. Returns *APIError 404 if no VM owns it.
func (c *Client) FindVMByInterface(ctx context.Context, interfaceID string) (*VM, error) {
	// The API doesn't expose an interface→vm filter. List and scan.
	// In practice the user has at most a few hundred VMs; pagination handled
	// by passing per_page=200 and walking pages until we find it or exhaust.
	page := uint64(0)
	for {
		q := url.Values{"page": {FormatPage(page)}, "per_page": {"200"}}
		var resp vmsListResponse
		if err := c.do(ctx, http.MethodGet, "/v2/vms", q, nil, &resp); err != nil {
			return nil, err
		}
		for i := range resp.Items {
			for _, ni := range resp.Items[i].NetworkInterfaces {
				if ni == interfaceID {
					return &resp.Items[i], nil
				}
			}
		}
		if resp.Pagination.CurrentPage+1 >= uint64(resp.Pagination.TotalPages) {
			return nil, &APIError{StatusCode: http.StatusNotFound, Message: "no VM owns interface " + interfaceID}
		}
		page++
	}
}
```

- [ ] **Step 9.2: Write the failing test**

Create `internal/provider/security_group_attachment_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccSecurityGroupAttachment_BindAndUnbind(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "cluster-A"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_vm" "app" {
  cluster_id       = "cluster-A"
  name             = "app"
  configuration_id = "cfg-1"
  boot_disk { storage_id = cloudless_storage.boot.id }
}

resource "cloudless_security_group" "web" {
  cluster_id = "cluster-A"
  name       = "web"
}

resource "cloudless_security_group_attachment" "att" {
  network_interface_id = cloudless_vm.app.network_interface_ids[0]
  security_group_id    = cloudless_security_group.web.id
}
`,
				Check: resource.TestCheckResourceAttrPair(
					"cloudless_security_group_attachment.att", "vm_id",
					"cloudless_vm.app", "id",
				),
			},
		},
	})
}
```

- [ ] **Step 9.3: Run, expect FAIL**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccSecurityGroupAttachment -v'
```
Expected: error mentioning unknown resource.

- [ ] **Step 9.4: Implement**

Create `internal/provider/security_group_attachment_resource.go`:

```go
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

func NewSecurityGroupAttachmentResource() resource.Resource { return &sgAttachmentResource{} }

type sgAttachmentResource struct{ c *client.Client }

type sgAttachmentModel struct {
	ID                 types.String `tfsdk:"id"`
	NetworkInterfaceID types.String `tfsdk:"network_interface_id"`
	SecurityGroupID    types.String `tfsdk:"security_group_id"`
	VMID               types.String `tfsdk:"vm_id"`
}

func (r *sgAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_group_attachment"
}

func (r *sgAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Bind a security group to a network interface (one SG per interface). VM ID is resolved at create-time and stored in state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"network_interface_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"security_group_id": schema.StringAttribute{
				Required: true,
				// In-place updatable: changes call PATCH with the new ID.
			},
			"vm_id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sgAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *sgAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sgAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err := r.c.FindVMByInterface(ctx, plan.NetworkInterfaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Find VM for interface failed", err.Error())
		return
	}
	sgID := plan.SecurityGroupID.ValueString()
	if err := r.c.UpdateVMInterface(ctx, vm.ID, plan.NetworkInterfaceID.ValueString(), &sgID); err != nil {
		resp.Diagnostics.AddError("Bind SG failed", err.Error())
		return
	}
	plan.VMID = types.StringValue(vm.ID)
	plan.ID = plan.NetworkInterfaceID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sgAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sgAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ifaces, err := r.c.ListVMInterfaces(ctx, state.VMID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("List interfaces failed", err.Error())
		return
	}
	for _, ni := range ifaces {
		if ni.ID != state.NetworkInterfaceID.ValueString() {
			continue
		}
		if ni.SecurityGroupID == nil || *ni.SecurityGroupID != state.SecurityGroupID.ValueString() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	// Interface itself disappeared.
	resp.State.RemoveResource(ctx)
}

func (r *sgAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sgAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.SecurityGroupID.Equal(state.SecurityGroupID) {
		sgID := plan.SecurityGroupID.ValueString()
		if err := r.c.UpdateVMInterface(ctx, state.VMID.ValueString(), state.NetworkInterfaceID.ValueString(), &sgID); err != nil {
			resp.Diagnostics.AddError("Update SG binding failed", err.Error())
			return
		}
	}
	// vm_id and network_interface_id are immutable.
	plan.VMID = state.VMID
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sgAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sgAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.UpdateVMInterface(ctx, state.VMID.ValueString(), state.NetworkInterfaceID.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unbind SG failed", err.Error())
	}
}

func (r *sgAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Accept either "<vm_id>:<network_interface_id>" or "<network_interface_id>".
	parts := strings.SplitN(req.ID, ":", 2)
	if len(parts) == 2 {
		resp.State.SetAttribute(ctx, path.Root("id"), parts[1])
		resp.State.SetAttribute(ctx, path.Root("network_interface_id"), parts[1])
		resp.State.SetAttribute(ctx, path.Root("vm_id"), parts[0])
		return
	}
	// Single-id form: resolve vm_id by scanning.
	ifaceID := req.ID
	vm, err := r.c.FindVMByInterface(ctx, ifaceID)
	if err != nil {
		resp.Diagnostics.AddError("Resolve vm_id from interface failed", err.Error())
		return
	}
	resp.State.SetAttribute(ctx, path.Root("id"), ifaceID)
	resp.State.SetAttribute(ctx, path.Root("network_interface_id"), ifaceID)
	resp.State.SetAttribute(ctx, path.Root("vm_id"), vm.ID)
	// security_group_id will be filled by Read on the next refresh.
}
```

- [ ] **Step 9.5: Register**

In `internal/provider/provider.go`, add `NewSecurityGroupAttachmentResource` to `Resources`.

- [ ] **Step 9.6: Run, expect PASS**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider/ -run TestAccSecurityGroupAttachment -v'
```
Expected: PASS.

- [ ] **Step 9.7: Add example**

Create `examples/resources/cloudless_security_group_attachment/resource.tf`:

```hcl
resource "cloudless_security_group_attachment" "web" {
  network_interface_id = cloudless_vm.app.network_interface_ids[0]
  security_group_id    = cloudless_security_group.web.id
}
```

- [ ] **Step 9.8: Final full-build sanity check**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./... && go vet ./... && go test ./...'
```
Expected: every package passes.

- [ ] **Step 9.9: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform
git add internal/client/client.go internal/provider/security_group_attachment_resource.go internal/provider/security_group_attachment_resource_test.go internal/provider/provider.go examples/resources/cloudless_security_group_attachment/
git commit -m "$(cat <<'EOF'
feat(sg_attachment): cloudless_security_group_attachment

Binds an SG to a network interface via PATCH. Resolves vm_id at create
time by listing VMs and matching the interface; stores it as a computed
attribute. Import accepts <iface> or <vm>:<iface>.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Spec coverage check

After completing all 9 tasks, verify against the spec sections:

| Spec section | Implemented in |
|---|---|
| `cloudless_security_group` resource | Task 4 |
| `cloudless_storage` resource | Task 5 |
| `cloudless_public_ip` resource | Task 6 |
| `cloudless_vm` updates (data_disk_ids, drop public_ip block) | Task 7 |
| `cloudless_vm_public_ip_attachment` | Task 8 |
| `cloudless_security_group_attachment` | Task 9 |
| `cloudless_cluster` data source (singular, errors on ambiguity) | Task 3 |
| `cloudless_clusters` data source (filterable list) | Task 3 |
| Subnet `cluster_id` Optional + derive from VPC | Task 2 |
| Mock server for unit testing | Task 1 (scaffold) + grown by every later task |

Phase 2 items (NOT covered here, will land in a separate plan):
- Validators package (UUID, CIDR, port spec, region code)
- Generated docs via tfplugindocs
- Acceptance tests for every resource (gated on TF_ACC=1)
- Unit tests for the existing phase-0 resources (ssh_key, vpc — vm/subnet covered by Tasks 7/2)
- CI workflow
- Release pipeline (.goreleaser.yml)
- README rewrite

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-04-cloudless-provider-phase1.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?





