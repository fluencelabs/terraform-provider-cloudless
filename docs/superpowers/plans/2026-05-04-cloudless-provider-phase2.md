# Cloudless Provider — Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take the feature-complete Phase 1 provider and harden it for publication: validators, generated docs, expanded test coverage (unit + acceptance), CI, release pipeline. End state is a provider that's ready to tag and publish to the Terraform Registry.

**Architecture:** Layer hardening on top of Phase 1's three-layer stack without rewriting it. Validators live in their own package and plug into existing schemas. Acceptance tests gate on `TF_ACC=1` so the unit-test suite stays fast and credential-free. Docs are generated from schema descriptions via `tfplugindocs`; CI runs build + vet + unit tests on every PR; `goreleaser` is configured but not triggered (first release tag is out-of-scope).

**Tech Stack:** Go 1.25 (existing), terraform-plugin-framework v1.19 (existing), terraform-plugin-framework-validators v0.19 (existing), terraform-plugin-testing (existing), `github.com/hashicorp/terraform-plugin-docs` (new), `goreleaser/goreleaser` (new, dev-shell only), GitHub Actions, `gnupg` (dev-shell, signing).

---

## Decomposition note

Phase 2 is the second of two implementation plans the umbrella spec promised. It assumes Phase 1 (`2026-05-04-cloudless-provider-phase1.md`) has shipped: 9 tasks, 17 commits, on master at SHA `6390273`.

Phase 2 ends when the repo is publishable: validators, docs, tests, CI, release pipeline. The actual first release tag (the `v0.1.0` cut + GPG-signed registry submission) is out-of-scope — that's a separate "publish" milestone once the registry namespace is decided.

## Phase-1 carry-forwards folded into this plan

The Phase 1 final review left four non-blocking items that this plan addresses opportunistically:

| Carry-forward | Lands in |
|---|---|
| Extract `security_group_translate.go` from the 487-line resource file | Task 1 (cleanup refactor) |
| Split `data_sources.go` into per-data-source files | Task 1 (cleanup refactor) |
| `clusters.go` mock: align to defensive `wireXOnce` pattern | Task 1 (cleanup refactor) |
| Mock `wire()` helpers: marshal under lock or deep-copy slices | Task 1 (cleanup refactor) |

Module path rename (`github.com/cloudless/...` placeholder) is **not** part of Phase 2 — defer until the registry namespace is decided.

## File structure overview

After Phase 2:

```
.
├── README.md                                    # final pass at end
├── go.mod / go.sum                              # tools.go pulls in tfplugindocs
├── tools.go                                     # CREATE: tfplugindocs import
├── flake.nix                                    # MODIFY: add goreleaser, gnupg
├── .goreleaser.yml                              # CREATE
├── .github/workflows/build.yml                  # CREATE
├── .github/CODEOWNERS                           # OPTIONAL
├── docs/                                        # CREATE (generated)
│   ├── index.md
│   ├── resources/
│   │   ├── ssh_key.md
│   │   ├── vpc.md
│   │   ├── subnet.md
│   │   ├── vm.md
│   │   ├── security_group.md
│   │   ├── storage.md
│   │   ├── public_ip.md
│   │   ├── vm_public_ip_attachment.md
│   │   └── security_group_attachment.md
│   └── data-sources/
│       ├── cluster.md
│       ├── clusters.md
│       ├── vm_configurations.md
│       └── default_images.md
├── templates/                                   # CREATE (overrides)
│   ├── index.md.tmpl
│   ├── resources.md.tmpl
│   └── data-sources.md.tmpl
├── examples/
│   ├── README.md
│   ├── provider/                                # CREATE
│   │   └── provider.tf
│   ├── resources/<name>/resource.tf             # existing per-resource
│   └── data-sources/<name>/data-source.tf       # CREATE
├── internal/
│   ├── client/                                  # unchanged in Phase 2
│   │   └── mock/
│   │       ├── server.go                        # MODIFY: wire() lock fix
│   │       ├── clusters.go                      # MODIFY: defensive lock pattern
│   │       └── ...
│   └── provider/
│       ├── ssh_key_resource.go                  # touched (validators applied)
│       ├── ssh_key_resource_test.go             # CREATE
│       ├── vpc_resource.go                      # touched (validators applied)
│       ├── vpc_resource_test.go                 # CREATE
│       ├── ...
│       ├── data_sources.go                      # SPLIT
│       ├── vm_configurations_data_source.go     # CREATE (split out)
│       ├── default_images_data_source.go        # CREATE (split out)
│       ├── security_group_resource.go           # MODIFY (translators extracted)
│       ├── security_group_translate.go          # CREATE (extracted helpers)
│       ├── validators/                          # CREATE (new package)
│       │   ├── doc.go
│       │   ├── uuid.go
│       │   ├── uuid_test.go
│       │   ├── cidr.go
│       │   ├── cidr_test.go
│       │   ├── port_spec.go
│       │   ├── port_spec_test.go
│       │   ├── region_code.go
│       │   └── region_code_test.go
│       └── acctest/                             # CREATE (acceptance test helper)
│           ├── harness.go
│           └── *_acc_test.go                    # CREATE per resource
```

## How to read this plan

Each task is one logical unit. Steps are 2-5 minutes each. **Run all Go commands inside `nix develop`** (`nix develop --command bash -c '...'` per command, or open a persistent shell).

Most tasks should land with a single commit; refactor cleanup (Task 1) and validator application (Task 2) are big enough to commit in sub-units.

---

## Task 1: Refactor cleanup (Phase-1 carry-forwards)

**Files:**
- Create: `internal/provider/security_group_translate.go`
- Modify: `internal/provider/security_group_resource.go` (remove translator helpers)
- Create: `internal/provider/vm_configurations_data_source.go`
- Create: `internal/provider/default_images_data_source.go`
- Delete: `internal/provider/data_sources.go` (or shrink to a doc comment)
- Modify: `internal/client/mock/clusters.go` (align defensive wire pattern)
- Modify: `internal/client/mock/{vm,storage,public_ip,security_groups,subnets,vpcs}.go` (marshal-in-lock)

These are non-functional changes. Tests must continue to pass byte-for-byte.

- [ ] **Step 1.1: Extract SG translators to their own file**

Move the following from `internal/provider/security_group_resource.go` to a new file `internal/provider/security_group_translate.go` (same package, same imports as needed):

- `func normalizeMode(s types.String) (string, error)`
- `func apiToMode(r client.SecurityGroupRules) string`
- `func buildRules(mode string, blocks []sgRule) (client.SecurityGroupRules, error)`
- `func translateRule(b sgRule) (client.SecurityGroupRule, error)`
- `func parsePorts(s string) (client.Ports, error)`
- `func rulesToModel(rules []client.SecurityGroupRule) []sgRule`
- `func portsToString(pk client.ProtocolKind) string`

The new file's imports will include `fmt`, `strconv`, `strings`, `github.com/hashicorp/terraform-plugin-framework/types`, and `github.com/cloudless/terraform-provider-cloudless/internal/client`. Drop those imports from `security_group_resource.go` if they become unused.

- [ ] **Step 1.2: Verify after extraction**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go build ./... && go vet ./... && go test ./... -count=1'
```
Expected: silent. All 19 tests pass.

- [ ] **Step 1.3: Split `data_sources.go` into per-data-source files**

Create `internal/provider/vm_configurations_data_source.go` with:
- `vmConfigsDS` struct
- `vmConfigModel`, `vmConfigsModel` types
- `NewVMConfigurationsDataSource()` constructor
- `Metadata`, `Schema`, `Configure`, `Read` methods

Move the corresponding code from `internal/provider/data_sources.go`. Keep the same package, same imports.

Create `internal/provider/default_images_data_source.go` with the analogous content for `defaultImagesDS`, `defaultImageModel`, `defaultImagesModel`, `NewDefaultImagesDataSource`.

After moving, `data_sources.go` will be empty except for `package provider`. Delete it:

```bash
rm internal/provider/data_sources.go
```

- [ ] **Step 1.4: Verify after data sources split**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go build ./... && go vet ./... && go test ./... -count=1'
```
Expected: silent. All 19 tests pass.

- [ ] **Step 1.5: Align `clusters.go` mock to defensive lock pattern**

Open `internal/client/mock/clusters.go`. The current `SeedCluster` and `SeedDatacenter` functions hold `s.mu` while calling `wireClustersOnce.Do(...)` and `wireDCsOnce.Do(...)` — same anti-pattern that was fixed for SG / storage / public_ip / VM. Refactor to the established pattern:

- A `wireClustersOnce()` method that calls `s.wireClustersOnce.Do(s.wireClusters)` with NO lock held.
- A `wireClusters()` method that takes `s.mu` only briefly for map init, then registers the `mux.HandleFunc` lock-free.
- `SeedCluster` / `SeedDatacenter` only mutate the maps under `s.mu`; do NOT call `Do(...)` from inside Seed.
- Move the `wireXOnce()` calls to `New()` in `server.go`, alongside the other `wireXOnce()` calls.

Result: `SeedCluster(id, name, dcID)` and `SeedDatacenter(id, country, city, slug)` keep the same signatures and behavior, but the route registration is hoisted to construction time.

- [ ] **Step 1.6: Verify after clusters mock refactor**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go build ./... && go vet ./... && go test ./... -count=1'
```
Expected: silent. All 19 tests pass (including the cluster data source tests which exercise the new wire pattern).

- [ ] **Step 1.7: Fix mock `wire()` lock-after-write race**

Several mock handlers (in `vm.go`, `storage.go`, `public_ip.go`, `security_groups.go`, `subnets.go`, `vpcs.go`) build a wire map that contains slices borrowed from the record (e.g. `rec.DataDisks`), then unlock, then call `s.writeJSON(out)`. If a concurrent request mutates the slice between unlock and JSON encode, you have a data race.

Fix: in every handler that writes a wire response, marshal the JSON inside the lock OR deep-copy the slices into the wire map. The cheaper fix is to keep the lock until after `s.writeJSON(...)`. Pattern:

```go
// before:
s.mu.Lock()
// ... mutate state ...
out := vmWire(rec)
s.mu.Unlock()
s.writeJSON(w, http.StatusOK, out)

// after:
s.mu.Lock()
defer s.mu.Unlock()
// ... mutate state ...
out := vmWire(rec)
s.writeJSON(w, http.StatusOK, out)
```

Apply to every `s.writeJSON(w, http.StatusOK, wireFunc(rec))` call site. Run `grep -rn 'writeJSON.*Wire(' internal/client/mock/` to find them.

The PATCH paths in `vm.go`, `storage.go`, `public_ip.go`, `security_groups.go` all need this fix. POST creates are mostly already fine because they write the just-created record before any other goroutine could see it, but apply the same pattern for symmetry.

- [ ] **Step 1.8: Verify after mock lock fix**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go build ./... && go vet ./... && go test ./... -race -count=1'
```
Expected: silent. All 19 tests pass under `-race`.

- [ ] **Step 1.9: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening
git add internal/provider/security_group_translate.go internal/provider/security_group_resource.go internal/provider/vm_configurations_data_source.go internal/provider/default_images_data_source.go internal/provider/data_sources.go internal/client/mock/
git commit -m "$(cat <<'EOF'
refactor: phase-1 carry-forward cleanups

- security_group: extract rule translators to security_group_translate.go
  (resource file drops from 487 → ~290 lines)
- data_sources.go: split per-data-source into vm_configurations_data_source.go
  and default_images_data_source.go; remove the catch-all file
- mock/clusters.go: align to the wireClustersOnce/wireClusters defensive
  pattern (lock-free Do; brief lock for map init)
- mock/*.go: keep s.mu held across writeJSON() to close a wire-after-unlock
  race that would only fire under concurrent test workloads

No behavior changes. 19 tests pass under -race.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Validators package + apply to schemas

**Files:**
- Create: `internal/provider/validators/doc.go`
- Create: `internal/provider/validators/uuid.go` and `uuid_test.go`
- Create: `internal/provider/validators/cidr.go` and `cidr_test.go`
- Create: `internal/provider/validators/port_spec.go` and `port_spec_test.go`
- Create: `internal/provider/validators/region_code.go` and `region_code_test.go`
- Modify: every resource and data-source schema to apply the new validators

The package implements `validator.String` from `terraform-plugin-framework/schema/validator`. Each validator is testable in isolation via the framework's `validator.StringRequest` / `validator.StringResponse` types.

- [ ] **Step 2.1: Create the package skeleton**

Create `internal/provider/validators/doc.go`:

```go
// Package validators provides reusable schema-attribute validators for the
// cloudless Terraform provider. Each validator implements
// validator.String (from terraform-plugin-framework/schema/validator) so it
// can be applied to schema.StringAttribute via Validators: []validator.String{...}.
package validators
```

- [ ] **Step 2.2: Implement UUID validator + test (TDD)**

Create `internal/provider/validators/uuid_test.go`:

```go
package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestUUID(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid lowercase", "92e6ed24-8bfa-4737-9409-a1aac994e1f5", false},
		{"valid uppercase", "92E6ED24-8BFA-4737-9409-A1AAC994E1F5", false},
		{"empty", "", false}, // Optional fields can be empty; let Required handle that.
		{"missing dashes", "92e6ed248bfa47379409a1aac994e1f5", true},
		{"too short", "92e6ed24-8bfa-4737-9409-a1aac994e1f", true},
		{"non-hex char", "92e6ed24-8bfa-4737-9409-a1aac994e1zz", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			UUID().ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("UUID(%q): error=%v, want %v; diags=%v", c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
```

- [ ] **Step 2.3: Run UUID test, expect FAIL**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go test ./internal/provider/validators/ -run TestUUID -v'
```
Expected: compile error `undefined: UUID`.

- [ ] **Step 2.4: Implement UUID validator**

Create `internal/provider/validators/uuid.go`:

```go
package validators

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type uuidValidator struct{}

// UUID returns a validator that accepts canonical 8-4-4-4-12 hex UUIDs.
// Empty strings pass — wire that with Required:true on the schema if needed.
func UUID() validator.String { return uuidValidator{} }

func (uuidValidator) Description(_ context.Context) string {
	return "value must be a UUID in 8-4-4-4-12 hex format"
}
func (v uuidValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (uuidValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	v := req.ConfigValue.ValueString()
	if v == "" {
		return
	}
	if !uuidPattern.MatchString(v) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid UUID", "expected an 8-4-4-4-12 hex UUID, got: "+v)
	}
}
```

- [ ] **Step 2.5: Run UUID test, expect PASS**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go test ./internal/provider/validators/ -run TestUUID -v'
```
Expected: 6 sub-tests pass.

- [ ] **Step 2.6: Implement CIDR validator + test**

Create `internal/provider/validators/cidr_test.go`:

```go
package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCIDR(t *testing.T) {
	cases := []struct {
		name    string
		family  string
		value   string
		wantErr bool
	}{
		{"v4 ok", "ipv4", "10.0.0.0/24", false},
		{"v4 with full bits", "ipv4", "0.0.0.0/0", false},
		{"v4 not v6", "ipv4", "2001:db8::/64", true},
		{"v6 ok", "ipv6", "2001:db8::/64", false},
		{"v6 not v4", "ipv6", "10.0.0.0/24", true},
		{"any v4", "any", "10.0.0.0/24", false},
		{"any v6", "any", "2001:db8::/64", false},
		{"missing prefix", "any", "10.0.0.0", true},
		{"bad prefix len", "ipv4", "10.0.0.0/33", true},
		{"empty", "any", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			CIDR(c.family).ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("CIDR(%q,%q): error=%v want %v; diags=%v", c.family, c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
```

Create `internal/provider/validators/cidr.go`:

```go
package validators

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// CIDR returns a validator that accepts a CIDR-formatted IP block.
// family is "ipv4", "ipv6", or "any".
func CIDR(family string) validator.String {
	return cidrValidator{family: family}
}

type cidrValidator struct{ family string }

func (v cidrValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be a CIDR block (%s)", v.family)
}
func (v cidrValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v cidrValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if val == "" {
		return
	}
	ip, _, err := net.ParseCIDR(val)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR", err.Error())
		return
	}
	is4 := ip.To4() != nil && !strings.Contains(val, ":")
	switch v.family {
	case "ipv4":
		if !is4 {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR family", "expected IPv4 CIDR, got "+val)
		}
	case "ipv6":
		if is4 {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR family", "expected IPv6 CIDR, got "+val)
		}
	case "any":
		// no further check
	default:
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid validator config", "unknown CIDR family: "+v.family)
	}
}
```

Run: `nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go test ./internal/provider/validators/ -run TestCIDR -v'`. Expected: 10 sub-tests pass.

- [ ] **Step 2.7: Implement port-spec validator + test**

Create `internal/provider/validators/port_spec_test.go`:

```go
package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPortSpec(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty", "", false},
		{"all", "all", false},
		{"single", "443", false},
		{"range", "8000-8100", false},
		{"single 0", "0", false},
		{"single 65535", "65535", false},
		{"single 65536", "65536", true},
		{"negative", "-1", true},
		{"reverse range", "100-50", true},
		{"non-numeric", "abc", true},
		{"trailing dash", "100-", true},
		{"extra dash", "1-2-3", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			PortSpec().ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("PortSpec(%q): error=%v want %v; diags=%v", c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
```

Create `internal/provider/validators/port_spec.go`:

```go
package validators

import (
	"context"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// PortSpec accepts: "" (empty), "all", a port number 0-65535, or a "min-max"
// range with min ≤ max and both in 0-65535.
func PortSpec() validator.String { return portSpecValidator{} }

type portSpecValidator struct{}

func (portSpecValidator) Description(_ context.Context) string {
	return `value must be "all", a port number, or a "min-max" range`
}
func (v portSpecValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (portSpecValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if val == "" || val == "all" {
		return
	}
	if !strings.Contains(val, "-") {
		n, err := strconv.ParseUint(val, 10, 16)
		if err != nil {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid port", "expected 0-65535, got "+val)
			return
		}
		_ = n
		return
	}
	parts := strings.Split(val, "-")
	if len(parts) != 2 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid port range", `expected "min-max"`)
		return
	}
	mn, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid port range", "min: "+parts[0]+" not a valid port")
		return
	}
	mx, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid port range", "max: "+parts[1]+" not a valid port")
		return
	}
	if mn > mx {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid port range", "min > max")
	}
}
```

Run: `nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go test ./internal/provider/validators/ -run TestPortSpec -v'`. Expected: 12 sub-tests pass.

- [ ] **Step 2.8: Implement region-code validator + test**

Create `internal/provider/validators/region_code_test.go`:

```go
package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestRegionCode(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty", "", false},
		{"DE", "DE", false},
		{"PL", "PL", false},
		{"lower", "de", true},
		{"single char", "D", true},
		{"three chars", "DEU", true},
		{"digits", "12", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validator.StringRequest{ConfigValue: types.StringValue(c.value)}
			resp := &validator.StringResponse{}
			RegionCode().ValidateString(context.Background(), req, resp)
			if got := resp.Diagnostics.HasError(); got != c.wantErr {
				t.Fatalf("RegionCode(%q): error=%v want %v; diags=%v", c.value, got, c.wantErr, resp.Diagnostics)
			}
		})
	}
}
```

Create `internal/provider/validators/region_code.go`:

```go
package validators

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// RegionCode validates an ISO 3166-1 alpha-2 country code (uppercase 2 letters).
func RegionCode() validator.String { return regionCodeValidator{} }

var regionPattern = regexp.MustCompile(`^[A-Z]{2}$`)

type regionCodeValidator struct{}

func (regionCodeValidator) Description(_ context.Context) string {
	return "value must be an ISO 3166-1 alpha-2 country code (e.g. DE, PL)"
}
func (v regionCodeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (regionCodeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	v := req.ConfigValue.ValueString()
	if v == "" {
		return
	}
	if !regionPattern.MatchString(v) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid region code", "expected 2 uppercase letters (ISO 3166-1 alpha-2), got "+v)
	}
}
```

Run: `nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go test ./internal/provider/validators/ -v'`. Expected: all 4 validators' tests pass.

- [ ] **Step 2.9: Apply validators to resource and data source schemas**

Add `validators.UUID()` to every `*_id` field. Add `validators.CIDR(...)` to subnet's `ipv4_cidr` / `ipv6_cidr` and SG rules' `cidr`. Add `validators.PortSpec()` to SG rules' `ports`. Add `validators.RegionCode()` to data sources' `region` / `regions` filters.

Concrete edits (each one is `import "github.com/cloudless/terraform-provider-cloudless/internal/provider/validators"` + a single field's `Validators: []validator.String{validators.UUID()}` (or analog)):

Resources:
- `vpc_resource.go`: `cluster_id` → UUID
- `subnet_resource.go`: `cluster_id`, `vpc_id` → UUID; `ipv4_cidr` → `CIDR("ipv4")`; `ipv6_cidr` → `CIDR("ipv6")`
- `security_group_resource.go`: `cluster_id` → UUID; rule blocks: `cidr` → `CIDR("any")`, `security_group_id` → UUID, `ports` → `PortSpec()`
- `storage_resource.go`: `cluster_id` → UUID
- `public_ip_resource.go`: `cluster_id` → UUID
- `vm_resource.go`: `cluster_id`, `configuration_id` → UUID; `boot_disk.storage_id` → UUID
- `vm_public_ip_attachment_resource.go`: `vm_id`, `public_ip_id` → UUID
- `security_group_attachment_resource.go`: `network_interface_id`, `security_group_id` → UUID
- (For list of UUIDs: `data_disk_ids`, `ssh_key_ids` — add a list-element validator wrapped via `listvalidator.ValueStringsAre(validators.UUID())`. This requires `import "github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"`.)

Data sources:
- `cluster_data_source.go`: `id` → UUID; `region` → `RegionCode()`; `dc_id` → UUID
- `clusters_data_source.go`: `regions` list → `listvalidator.ValueStringsAre(validators.RegionCode())`

For each schema edit, find the existing `schema.StringAttribute{Required: true, ...}` block and add `Validators: []validator.String{validators.UUID()}` (or whichever applies). Run `go build ./...` after each file to catch import / compile issues incrementally.

- [ ] **Step 2.10: Run full suite, expect PASS**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go build ./... && go vet ./... && go test ./... -count=1'
```
Expected: silent build/vet, all 19 + 4 (validator package) = 23 tests pass.

- [ ] **Step 2.11: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening
git add internal/provider/validators/ internal/provider/
git commit -m "$(cat <<'EOF'
feat(validators): add UUID, CIDR, PortSpec, RegionCode + apply across schemas

New internal/provider/validators package with 4 reusable string
validators, each unit-tested. Applied to every UUID-shaped field
across resources and data sources, to subnet CIDRs, to SG rules' cidr
and ports, and to the cluster-data-source region filter.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Phase-0 unit tests (ssh_key, vpc)

**Files:**
- Create: `internal/provider/ssh_key_resource_test.go`
- Create: `internal/provider/vpc_resource_test.go`

VM and subnet already have unit tests from Phase 1. SSH key and VPC do not.

- [ ] **Step 3.1: Write SSH key test**

Create `internal/provider/ssh_key_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccSSHKey_CreateAndRead(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_ssh_key" "me" {
  name       = "demo"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1DjqOOxINs78oU3f7PJXIyq9uiNocNVhXNx user@example.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_ssh_key.me", "name", "demo"),
					resource.TestCheckResourceAttrSet("cloudless_ssh_key.me", "id"),
					resource.TestCheckResourceAttrSet("cloudless_ssh_key.me", "fingerprint"),
				),
			},
		},
	})
}
```

The mock will need an SSH-key endpoint set. Phase 1's mock currently has VPC, subnet, SG, storage, public_ip, VM, clusters, datacenters — but not ssh_keys. Add it now.

Create `internal/client/mock/ssh_keys.go` (mirrors the storage/public_ip pattern):

```go
package mock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type sshKeyRecord struct {
	ID, UserID, Name, PublicKey, Algorithm, Fingerprint string
}

func (s *Server) wireSSHKeysOnce() { s.sshKeyWiring.Do(s.wireSSHKeys) }

func (s *Server) wireSSHKeys() {
	s.mu.Lock()
	if s.sshKeyMap == nil {
		s.sshKeyMap = map[string]*sshKeyRecord{}
	}
	s.mu.Unlock()

	s.mux.HandleFunc("/v1/ssh_keys", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Name      string `json:"name"`
				PublicKey string `json:"publicKey"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			defer s.mu.Unlock()
			id := newID()
			sum := sha256.Sum256([]byte(body.PublicKey))
			rec := &sshKeyRecord{
				ID:          id,
				UserID:      "test-user",
				Name:        body.Name,
				PublicKey:   body.PublicKey,
				Algorithm:   "ssh-ed25519",
				Fingerprint: "SHA256:" + hex.EncodeToString(sum[:8]),
			}
			s.sshKeyMap[id] = rec
			s.writeJSON(w, http.StatusOK, sshKeyWire(rec))
		case http.MethodGet:
			want := r.URL.Query().Get("ids")
			s.mu.Lock()
			defer s.mu.Unlock()
			items := []map[string]any{}
			for id, k := range s.sshKeyMap {
				if want != "" && id != want {
					continue
				}
				items = append(items, sshKeyWire(k))
			}
			s.writeJSON(w, http.StatusOK, map[string]any{
				"items":      items,
				"pagination": map[string]int{"totalRecords": len(items), "filteredRecords": len(items), "totalPages": 1, "currentPage": 0, "perPage": 100},
			})
		default:
			s.notFound(w, r)
		}
	})
	s.mux.HandleFunc("/v1/ssh_keys/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ IDs []string `json:"ids"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, id := range body.IDs {
			delete(s.sshKeyMap, id)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func sshKeyWire(rec *sshKeyRecord) map[string]any {
	return map[string]any{
		"id":          rec.ID,
		"userId":      rec.UserID,
		"name":        rec.Name,
		"publicKey":   rec.PublicKey,
		"algorithm":   rec.Algorithm,
		"fingerprint": rec.Fingerprint,
	}
}
```

Add to `Server` struct in `server.go`: `sshKeyMap map[string]*sshKeyRecord`, `sshKeyWiring sync.Once`. Call `s.wireSSHKeysOnce()` in `New()`.

- [ ] **Step 3.2: Run SSH key test, expect PASS**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go test ./internal/provider/ -run TestAccSSHKey -v'
```
Expected: PASS.

- [ ] **Step 3.3: Write VPC test**

Create `internal/provider/vpc_resource_test.go`:

```go
package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

func TestAccVPC_CreateUpdateRename(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_vpc" "main" {
  cluster_id = "92e6ed24-8bfa-4737-9409-a1aac994e1f5"
  name       = "main"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vpc.main", "name", "main"),
					resource.TestCheckResourceAttr("cloudless_vpc.main", "status", "ready"),
				),
			},
			{
				Config: `
resource "cloudless_vpc" "main" {
  cluster_id = "92e6ed24-8bfa-4737-9409-a1aac994e1f5"
  name       = "renamed"
}
`,
				Check: resource.TestCheckResourceAttr("cloudless_vpc.main", "name", "renamed"),
			},
		},
	})
}
```

The mock's VPC endpoints currently only handle GET (added in Phase 1 Task 2 to support subnet derive). Extend `internal/client/mock/vpcs.go` to handle POST, PATCH, POST-delete. Add corresponding handlers following the SG/storage/public_ip pattern.

- [ ] **Step 3.4: Extend VPC mock for full CRUD**

Modify `internal/client/mock/vpcs.go` `wireVPCs` to add POST `/v1/vpcs`, PATCH `/v1/vpcs/{id}`, POST `/v1/vpcs/delete` handlers. Pattern same as storage/public_ip — POST creates a record, PATCH mutates `name` and `enableExternal`, delete removes. All under `s.mu`. Wire-after-write is fine (lock held until writeJSON returns thanks to Task 1.7 fix).

- [ ] **Step 3.5: Run VPC test**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go test ./internal/provider/ -run TestAccVPC -v'
```
Expected: PASS.

- [ ] **Step 3.6: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening
git add internal/provider/ssh_key_resource_test.go internal/provider/vpc_resource_test.go internal/client/mock/
git commit -m "$(cat <<'EOF'
test: unit tests for ssh_key and vpc resources

Mock gains a sshKeys handler (POST/GET/POST-delete) with synthesized
fingerprint. Mock vpcs handler grows POST/PATCH/POST-delete (was
GET-only). Two new acceptance tests bring phase-0 resource coverage
to parity with phase-1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Acceptance test harness + per-resource acc tests

**Files:**
- Create: `internal/provider/acctest/harness.go`
- Create: one `*_acc_test.go` next to each resource (~9 files)

Acceptance tests run against the real Fluence API. They MUST be opt-in via `TF_ACC=1` and `FLUENCE_API_KEY` env vars; without those they call `t.Skip(...)`.

Per spec D4: these are NEVER run in CI. The CI workflow (Task 6) explicitly does NOT pass `TF_ACC=1`.

- [ ] **Step 4.1: Create the acceptance harness**

Create `internal/provider/acctest/harness.go`:

```go
// Package acctest provides a setup helper for acceptance tests that hit the
// real Fluence API. Tests skip unless TF_ACC=1 and FLUENCE_API_KEY are set.
package acctest

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/cloudless/terraform-provider-cloudless/internal/provider"
)

// Setup ensures TF_ACC=1 and FLUENCE_API_KEY are set, returning provider
// factories pointed at the real API. Calls t.Skip() with a clear reason
// otherwise.
func Setup(t *testing.T) map[string]func() (tfprotov6.ProviderServer, error) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run acceptance tests")
	}
	if os.Getenv("FLUENCE_API_KEY") == "" {
		t.Skip("set FLUENCE_API_KEY to run acceptance tests")
	}
	return map[string]func() (tfprotov6.ProviderServer, error){
		"cloudless": providerserver.NewProtocol6WithError(provider.New("acc")()),
	}
}

// Ctx returns a context for use inside Check funcs.
func Ctx() context.Context { return context.Background() }
```

(`provider.New("acc")` returns a factory that uses the real api_key from env. Verify this matches the existing `provider.New(version) func() provider.Provider` signature; if it doesn't, adapt.)

- [ ] **Step 4.2: Verify the harness builds and skips correctly**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go build ./...'
```
Expected: silent.

- [ ] **Step 4.3: Add acceptance test for `cloudless_ssh_key`**

Create `internal/provider/ssh_key_resource_acc_test.go`:

```go
package provider_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/cloudless/terraform-provider-cloudless/internal/provider/acctest"
)

func TestAccSSHKey_RealAPI(t *testing.T) {
	factories := acctest.Setup(t)
	name := fmt.Sprintf("tf-acc-ssh-%d", rand.Int63())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "cloudless_ssh_key" "me" {
  name       = %q
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1DjqOOxINs78oU3f7PJXIyq9uiNocNVhXNx tf-acc"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_ssh_key.me", "name", name),
					resource.TestCheckResourceAttrSet("cloudless_ssh_key.me", "fingerprint"),
				),
			},
			{
				ResourceName:      "cloudless_ssh_key.me",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
```

- [ ] **Step 4.4: Add acceptance tests for the remaining resources**

Create one `*_acc_test.go` file per resource, modeled on the SSH key example. For resources that need a `cluster_id`, list it via `data.cloudless_cluster` first:

- `vpc_resource_acc_test.go`
- `subnet_resource_acc_test.go`
- `security_group_resource_acc_test.go` — covers all three modes (allow_all default, allow_listed with one rule, deny_all)
- `storage_resource_acc_test.go` — Create + resize
- `public_ip_resource_acc_test.go`
- `vm_resource_acc_test.go` — Create + data_disk_ids smart Update
- `vm_public_ip_attachment_resource_acc_test.go`
- `security_group_attachment_resource_acc_test.go`

Each uses `acctest.Setup(t)` for the factories and `tf-acc-<random>-` name prefix to avoid collisions.

The skill assumes you know the patterns from the SSH key example. For VM, use a real cluster + configuration_id from the `cloudless_cluster` and `cloudless_vm_configurations` data sources at the top of the test config.

Each test SHOULD have `ImportState: true, ImportStateVerify: true` step where applicable to exercise import paths.

- [ ] **Step 4.5: Verify the acc tests skip when TF_ACC is unset (the default)**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go test ./internal/provider/ -count=1 2>&1 | grep -c "SKIP"'
```
Expected: roughly 9 SKIP lines (one per resource's acc test). Exact number varies depending on how Go test framework reports skips.

- [ ] **Step 4.6: Document running acc tests in README**

Add a section to `README.md`:

```markdown
## Running acceptance tests

Acceptance tests hit the real Fluence API. They are gated on
`TF_ACC=1` and `FLUENCE_API_KEY`. They are NOT run in CI.

```bash
TF_ACC=1 FLUENCE_API_KEY=... go test ./... -run TestAcc -timeout 60m
```

Each acc test names its resources with a `tf-acc-<random>-` prefix.
The framework destroys resources at the end of every TestStep, but
if a test panics or the process is killed, manually clean up with
the API.
```

- [ ] **Step 4.7: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening
git add internal/provider/acctest/ internal/provider/*_acc_test.go README.md
git commit -m "$(cat <<'EOF'
test(acc): acceptance tests for every resource, gated on TF_ACC=1

acctest.Setup(t) skips unless TF_ACC=1 and FLUENCE_API_KEY are set.
Per-resource acc tests cover lifecycle + import (where applicable).
Never run in CI; documented opt-in process in README.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: tfplugindocs setup + generated docs

**Files:**
- Create: `tools.go`
- Create: `templates/index.md.tmpl`
- Create: `templates/resources.md.tmpl`
- Create: `templates/data-sources.md.tmpl`
- Create: `examples/provider/provider.tf`
- Create: `examples/data-sources/<name>/data-source.tf` for each data source
- Create: `docs/` (generated; commit it)
- Modify: `flake.nix` (add `tfplugindocs`)
- Modify: `Makefile` or top-level go directive (add `make docs` or `go generate ./...`)

The plan uses `tfplugindocs` (https://github.com/hashicorp/terraform-plugin-docs). It reads schemas via the provider binary, combines with `templates/` and `examples/` to produce `docs/`.

- [ ] **Step 5.1: Add tfplugindocs to dev shell**

Modify `flake.nix`. Add `pkgs.terraform-plugin-docs` to the dev shell `packages` list:

```nix
default = pkgs.mkShell {
  packages = [ pkgs.terraform pkgs.go pkgs.terraform-plugin-docs ];
};
```

Verify: `nix develop --command bash -c 'tfplugindocs --version'`. Expected: a version string. If `terraform-plugin-docs` isn't available in nixpkgs at the pinned version, fall back to `go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs` and add it to `tools.go` (next step).

- [ ] **Step 5.2: Create tools.go**

Create `tools.go` at the repo root:

```go
//go:build tools

package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
```

Run: `nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && go mod tidy'`. The `terraform-plugin-docs` dependency joins `go.mod` as an indirect tools dep.

- [ ] **Step 5.3: Create the example structure**

`tfplugindocs` looks for examples at:
- `examples/provider/provider.tf`
- `examples/resources/<name>/resource.tf` (Phase 1 created these)
- `examples/data-sources/<name>/data-source.tf` (need to create)

Create `examples/provider/provider.tf`:

```hcl
provider "cloudless" {
  api_key  = var.fluence_api_key  # or set FLUENCE_API_KEY
  endpoint = "https://api.fluence.dev"  # optional override
}

variable "fluence_api_key" {
  type        = string
  sensitive   = true
  description = "Fluence API key. Can also be set via FLUENCE_API_KEY env var."
}
```

Create `examples/data-sources/cloudless_cluster/data-source.tf`:

```hcl
data "cloudless_cluster" "main" {
  region = "DE"
}
```

Create `examples/data-sources/cloudless_clusters/data-source.tf`:

```hcl
data "cloudless_clusters" "eu" {
  regions = ["DE", "PL"]
}
```

Create `examples/data-sources/cloudless_vm_configurations/data-source.tf`:

```hcl
data "cloudless_vm_configurations" "all" {}
```

Create `examples/data-sources/cloudless_default_images/data-source.tf`:

```hcl
data "cloudless_default_images" "all" {}
```

- [ ] **Step 5.4: Create template overrides**

Create `templates/index.md.tmpl`:

```
---
page_title: "cloudless Provider"
description: |-
  Manage compute resources on the Fluence decentralized compute marketplace via Terraform.
---

# cloudless Provider

The cloudless provider manages compute resources on the [Fluence](https://fluence.dev) decentralized compute marketplace, including VMs, networking (VPC / subnet / security groups / public IPs), and block storage.

## Example usage

{{ tffile "examples/provider/provider.tf" }}

{{ .SchemaMarkdown | trimspace }}
```

Create `templates/resources.md.tmpl` (one template applied to all resources):

```
---
page_title: "{{.Name}} {{.Type}} - {{.ProviderName}}"
description: |-
  {{ .Description | plainmarkdown | trimspace | prefixlines "  " }}
---

# {{.Name}} ({{.Type}})

{{ .Description | trimspace }}

{{ if .HasExample -}}
## Example Usage

{{tffile .ExampleFile }}
{{- end }}

{{ .SchemaMarkdown | trimspace }}

{{ if .HasImport -}}
## Import

Import is supported using the following syntax:

{{codefile "shell" .ImportFile}}
{{- end }}
```

Create `templates/data-sources.md.tmpl` (mirror the resources template but with `{{.Type}} = "Data Source"`).

- [ ] **Step 5.5: Generate docs**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && tfplugindocs generate --provider-name cloudless'
```

Expected: `docs/` directory created with `index.md`, `resources/<name>.md` (×9), `data-sources/<name>.md` (×4).

If `tfplugindocs` errors about missing examples or schemas, fix the closest schema description and re-run.

- [ ] **Step 5.6: Spot-check the generated docs**

Open `docs/resources/security_group.md` and confirm it includes:
- The `ingress_mode` / `egress_mode` enum description
- The example HCL from `examples/resources/cloudless_security_group/resource.tf`
- The list of attributes with their types and descriptions

Open `docs/data-sources/cluster.md` and confirm `region` mentions ISO 3166-1 alpha-2.

- [ ] **Step 5.7: Add `make docs` target**

Create `Makefile`:

```make
.PHONY: docs build test fmt vet

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./... -count=1

docs:
	tfplugindocs generate --provider-name cloudless

fmt:
	go fmt ./...
```

- [ ] **Step 5.8: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening
git add tools.go go.mod go.sum flake.nix templates/ examples/provider/ examples/data-sources/ docs/ Makefile
git commit -m "$(cat <<'EOF'
docs: tfplugindocs setup + generated docs/

Adds tools.go (tfplugindocs as a build dep), templates/ (override
files), examples/ (provider + data-source examples), and the
generated docs/ directory committed in. Makefile target `make docs`
regenerates them. Phase 2 docs are read-only outputs of the schema
descriptions; treat schema changes as the source of truth and re-run
make docs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: CI workflow

**Files:**
- Create: `.github/workflows/build.yml`

- [ ] **Step 6.1: Write the workflow**

Create `.github/workflows/build.yml`:

```yaml
name: build

on:
  push:
    branches: [main, master]
  pull_request:
    branches: [main, master]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
          check-latest: true
      - name: Build
        run: go build ./...
      - name: Vet
        run: go vet ./...
      - name: Test
        run: go test ./... -count=1 -timeout 5m
        env:
          # Acceptance tests are explicitly disabled in CI per spec D4.
          TF_ACC: ""
```

- [ ] **Step 6.2: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening
git add .github/workflows/build.yml
git commit -m "$(cat <<'EOF'
ci: build workflow on PRs and master pushes

setup-go 1.25 → go build / go vet / go test (unit only). Acceptance
tests are explicitly NOT run in CI per spec D4 (they need real API
credentials and a budget — local-only for now).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Release pipeline (goreleaser, configured but not triggered)

**Files:**
- Create: `.goreleaser.yml`
- Modify: `flake.nix` (add goreleaser, gnupg)

`goreleaser` is configured to produce signed multi-OS multi-arch artifacts following the Terraform Registry's expected layout. NOT triggered in this spec — the first release tag is its own milestone.

- [ ] **Step 7.1: Add goreleaser + gnupg to dev shell**

Modify `flake.nix` `packages` list:

```nix
packages = [ pkgs.terraform pkgs.go pkgs.terraform-plugin-docs pkgs.goreleaser pkgs.gnupg ];
```

Verify: `nix develop --command bash -c 'goreleaser --version && gpg --version | head -1'`. Expected: version strings.

- [ ] **Step 7.2: Write `.goreleaser.yml`**

Create `.goreleaser.yml`:

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - env:
      - CGO_ENABLED=0
    mod_timestamp: "{{ .CommitTimestamp }}"
    flags:
      - -trimpath
    ldflags:
      - "-s -w -X main.version={{.Version}}"
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    binary: "{{ .ProjectName }}_v{{ .Version }}"

archives:
  - format: zip
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "{{ .ProjectName }}_{{ .Version }}_SHA256SUMS"
  algorithm: sha256

signs:
  - artifacts: checksum
    args:
      - "--batch"
      - "--local-user"
      - "{{ .Env.GPG_FINGERPRINT }}"
      - "--output"
      - "${signature}"
      - "--detach-sign"
      - "${artifact}"

release:
  extra_files:
    - glob: "terraform-registry-manifest.json"
      name_template: "{{ .ProjectName }}_{{ .Version }}_manifest.json"

changelog:
  disable: true
```

- [ ] **Step 7.3: Add the registry manifest**

Create `terraform-registry-manifest.json`:

```json
{
  "version": 1,
  "metadata": {
    "protocol_versions": ["6.0"]
  }
}
```

- [ ] **Step 7.4: Smoke-test goreleaser config**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening && goreleaser check'
```
Expected: `0 errors`. If errors, fix and re-run.

`goreleaser release --snapshot --skip=publish,sign --clean` would actually build all targets locally without publishing or signing — useful as a manual smoke test, but it's slow and not part of this commit.

- [ ] **Step 7.5: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening
git add .goreleaser.yml terraform-registry-manifest.json flake.nix
git commit -m "$(cat <<'EOF'
release: goreleaser config (multi-OS / arch, GPG-signed checksums)

Configured but not triggered. First release tag is a follow-up that
needs the registry namespace and a real GPG key. flake.nix gains
goreleaser and gnupg so `nix develop` is one-stop. terraform-registry-
manifest.json declares protocol 6.0.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: README final pass

**Files:**
- Modify: `README.md`

- [ ] **Step 8.1: Rewrite README to reflect Phase 2 state**

Replace the bulk of `README.md` with a complete, publishable version:

```markdown
# terraform-provider-cloudless

A Terraform provider for the [Fluence](https://fluence.dev) decentralized
compute marketplace.

> Status: ready to publish. Phase 2 (validators, generated docs, CI,
> goreleaser) is complete; the first release tag is a follow-up.

## Resources

| Resource | Description |
| --- | --- |
| `cloudless_ssh_key` | SSH public key reusable across VMs. |
| `cloudless_vpc` | A VPC on a chosen cluster. |
| `cloudless_subnet` | A subnet inside a VPC. `cluster_id` derived from VPC if unset. |
| `cloudless_security_group` | Firewall rules. `ingress_mode` / `egress_mode` enum: `allow_all` (default) / `allow_listed` / `deny_all`. |
| `cloudless_storage` | Block storage volume. `volume_gb` is in-place resizable via PATCH. |
| `cloudless_public_ip` | Static IPv4 address. |
| `cloudless_vm` | A VM. Boot disk inline or referenced by ID. `data_disk_ids` smart-Updates without VM recreation. |
| `cloudless_vm_public_ip_attachment` | Bind a public IP to a VM. |
| `cloudless_security_group_attachment` | Bind a security group to a VM's network interface. |

## Data sources

| Data source | Description |
| --- | --- |
| `cloudless_cluster` | Look up exactly one cluster by `region` / `city_code` / `name` / `id`. Errors on ambiguity. |
| `cloudless_clusters` | List clusters with optional `regions` / `city_codes` / `names` AND-composed filters. |
| `cloudless_vm_configurations` | All VM presets (CPU/RAM). |
| `cloudless_default_images` | Curated default OS images. |

## Provider configuration

```hcl
provider "cloudless" {
  api_key  = var.fluence_api_key  # or set FLUENCE_API_KEY
  endpoint = "https://api.fluence.dev"  # optional override
}
```

## Getting started

```bash
export FLUENCE_API_KEY=...
cd examples
terraform init && terraform apply
```

`examples/main.tf` provisions an SSH key, a VPC + subnet, a `cloudless_storage`
boot disk, and a small VM that references the storage by ID.

Per-resource and per-data-source examples live in `examples/resources/` and
`examples/data-sources/`.

## Local development

This repo ships a Nix flake with all tooling:

```bash
nix develop
go build ./...
go test ./...
```

Tools available in the dev shell: `terraform`, `go`, `tfplugindocs`,
`goreleaser`, `gnupg`.

To use the freshly-built provider against a Terraform config without
publishing:

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "registry.terraform.io/cloudless/cloudless" = "/path/to/$GOPATH/bin"
  }
  direct {}
}
```

```bash
go install ./...
terraform plan
```

## Running acceptance tests

Acceptance tests hit the real Fluence API. They are gated on `TF_ACC=1` +
`FLUENCE_API_KEY`. They are NOT run in CI.

```bash
TF_ACC=1 FLUENCE_API_KEY=... go test ./... -run TestAcc -timeout 60m
```

Each acc test names resources with a `tf-acc-<random>-` prefix. The
framework destroys resources at the end of every TestStep, but if a test
panics or the process is killed, manually clean up with the API.

## Documentation

Docs are generated from schema descriptions via `tfplugindocs`. Regenerate
with `make docs`. Generated `docs/` is committed; the registry serves them
at publish time.

## Releasing (out of scope for now)

`goreleaser release --clean` builds signed multi-arch artifacts. Requires
`GPG_FINGERPRINT` env. The first release tag is a follow-up that needs the
registry namespace and a real GPG key.
```

- [ ] **Step 8.2: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase2-hardening
git add README.md
git commit -m "$(cat <<'EOF'
docs: README final pass for Phase 2 ship

Reflects feature-complete state: resource and data-source tables,
acc-test process, dev-shell tooling, doc-gen flow, release pipeline
status.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Spec coverage check

After completing all 8 tasks, verify against the umbrella spec's Phase 2 section:

| Spec Phase 2 plan item | Implemented in |
|---|---|
| Validators package + apply | Task 2 |
| Generate docs via tfplugindocs | Task 5 |
| Acceptance tests for every resource | Task 4 |
| Unit tests for existing phase-0 resources | Task 3 |
| CI workflow | Task 6 |
| Release pipeline | Task 7 |
| README rewrite | Task 8 |
| **Carry-forwards from Phase 1 reviews:** | |
| SG translators split | Task 1 (Step 1.1) |
| `data_sources.go` split | Task 1 (Step 1.3) |
| `clusters.go` defensive lock pattern | Task 1 (Step 1.5) |
| Mock wire-after-unlock fix | Task 1 (Step 1.7) |

Out-of-scope for Phase 2 (intentional):
- First release tag (needs registry namespace + real GPG key)
- CI-gated acceptance tests (D4)
- Module path rename (defer until namespace decided)

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-04-cloudless-provider-phase2.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
