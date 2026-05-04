# Cloudless Provider — Phase 4 Review Followups Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land all 17 findings from the 2026-05-09 dual-plugin review (golang-skills + terraform-provider-development), grouped into three commits by severity (Must Fix / Should Fix / Nits) with no behavior change beyond the bug fixes themselves.

**Architecture:** Three semantic commits, each containing several focused tasks. No new packages. Schema change in exactly one resource (`cloudless_ssh_key.name` → ForceNew). All other changes are internal: error handling, test naming, helper introduction/removal, mock failure injection.

**Tech Stack:** Go 1.25 (existing), terraform-plugin-framework v1.19, terraform-plugin-testing v1.16, hashicorp/terraform-plugin-log/tflog v0.10 (already in `go.sum` indirect; will become a direct dep).

---

## Decomposition note

The findings split cleanly along behavior lines:

- **Must Fix** (Tasks 1-3): correctness — a permanent diff, a silenced error, a partial-failure window.
- **Should Fix** (Tasks 4-10): hygiene that affects readers/operators — `errors.As`, test naming, `CheckDestroy`, pagination, version sentinel, RNG quality, parallelism.
- **Nits** (Tasks 11-17): cosmetic or dead code.

Tasks within a category are independent — they can land in any order before that category's commit. Run them top-down for least churn.

## File structure overview

After Phase 4 the diff touches:

```
.
├── internal/client/client.go                                      # MODIFY: errors.As, named storage body, FindVMByInterface comment
├── internal/client/mock/vm.go                                     # MODIFY: optional storage-add failure injection (one bool flag)
├── internal/provider/util.go                                      # MODIFY: delete pathRoot, delete notFoundOrRemoved, add tflog.Debug to waitFor
├── internal/provider/provider.go                                  # MODIFY: NewWithClient takes version, replace pathRoot call
├── internal/provider/ssh_key_resource.go                          # MODIFY: name → RequiresReplace
├── internal/provider/ssh_key_resource_test.go                     # MODIFY: rename, migrate to ConfigStateChecks
├── internal/provider/security_group_resource.go                   # MODIFY: handle normalizeMode err in Update; only-changed payload
├── internal/provider/storage_resource.go                          # MODIFY: drop unused initial allocation
├── internal/provider/vpc_resource.go                              # MODIFY: drop unused initial allocation
├── internal/provider/vm_resource.go                               # MODIFY: refresh state on partial-progress error
├── internal/provider/vm_resource_test.go                          # MODIFY: add VM Update partial-progress regression test
├── internal/provider/{vpc,subnet,vm,security_group,security_group_attachment,storage,public_ip,ssh_key,vm_public_ip_attachment}_resource_test.go
│                                                                  # MODIFY: rename TestAcc* → TestUnit* (mock-backed only)
├── internal/provider/{cluster,clusters}_data_source_test.go       # MODIFY: rename TestAcc* → TestUnit* if mock-backed
├── internal/provider/validator_apply_test.go                      # MODIFY: rename TestAcc* → TestUnit*
├── internal/provider/{ssh_key,vpc,subnet,vm,security_group,security_group_attachment,storage,public_ip,vm_public_ip_attachment}_resource_acc_test.go
│                                                                  # MODIFY: add CheckDestroy, switch math/rand → acctest.RandStringFromCharSet
└── docs/superpowers/plans/2026-05-09-cloudless-provider-phase4-review-followups.md
                                                                   # this file
```

## How to read this plan

Each task is a single logical change. Steps are 2-5 minute actions. Run all Go commands inside `nix develop`. Pattern:

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase4-review-followups && <command>'
```

If you are not running in a worktree, omit the `cd` segment — but a worktree is recommended.

Three commit checkpoints, at the end of:
- Task 3 (Must Fix bundle)
- Task 10 (Should Fix bundle)
- Task 17 (Nits bundle)

Between commits, the entire `go test ./...` suite must remain green.

---

# Commit 1 — Must Fix

## Task 1: `cloudless_ssh_key.name` → ForceNew

**Files:**
- Modify: `internal/provider/ssh_key_resource.go:45-48`
- Modify: `internal/provider/ssh_key_resource.go:115-127` (delete the no-op-writer comment + simplify Update)
- Modify: `internal/provider/ssh_key_resource_test.go` (mock test that covers rename → recreate)

The Fluence API has no PATCH for SSH keys. The current schema lets the user submit a `name` change Update can't honor; the no-op Update creates a permanent diff. Make `name` ForceNew so a rename = recreate, which the API does support.

- [ ] **Step 1.1: Add a regression test that asserts rename forces replacement**

Open `internal/provider/ssh_key_resource_test.go` and add:

```go
func TestUnitSSHKey_RenameForcesReplacement(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	cfg := func(name string) string {
		return fmt.Sprintf(`
resource "cloudless_ssh_key" "me" {
  name       = %q
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1DjqOOxINs78oU3f7PJXIyq9uiNocNVhXNx user@example.com"
}
`, name)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{Config: cfg("first")},
			{
				Config: cfg("second"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cloudless_ssh_key.me", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})
}
```

Add the imports at the top of the file (the existing test only needs `tfharness` and `resource`):

```go
import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)
```

- [ ] **Step 1.2: Run the test and confirm it fails**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run TestUnitSSHKey_RenameForcesReplacement -v'
```

Expected: FAIL — the plan reports `Update` (no replacement) instead of `DestroyBeforeCreate`.

- [ ] **Step 1.3: Add `RequiresReplace` to `name`**

In `internal/provider/ssh_key_resource.go`, replace the `name` schema entry (currently lines 45-48):

```go
"name": schema.StringAttribute{
    Required:    true,
    Description: "Human-readable name shown in the Fluence UI and CLI. Changing forces replacement; the Fluence API has no rename endpoint.",
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.RequiresReplace(),
    },
},
```

- [ ] **Step 1.4: Simplify `Update` (now genuinely unreachable for in-place changes)**

Replace `Update` (lines 115-127) with:

```go
// Update is a no-op writer. Both Required attributes (name, public_key) carry
// RequiresReplace plan modifiers, so any user-driven change forces recreate.
// This implementation only exists to satisfy the resource interface.
func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
    var plan sshKeyModel
    resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
    if resp.Diagnostics.HasError() {
        return
    }
    resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

(The body is unchanged; only the comment is updated to reflect the new schema.)

- [ ] **Step 1.5: Run the regression test and confirm pass**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run TestUnitSSHKey_RenameForcesReplacement -v'
```

Expected: PASS — plan now reports `DestroyBeforeCreate` for the name change.

- [ ] **Step 1.6: Run the rest of the SSH key tests**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run SSHKey -v'
```

Expected: all pass. The existing `TestAccSSHKey_CreateAndRead` (which we will rename in Task 5) still creates with name "demo" only, so no rename happens.

---

## Task 2: SG `Update` handles `normalizeMode` errors

**Files:**
- Modify: `internal/provider/security_group_resource.go:206-243`

`Create` handles `normalizeMode` errors via `AddAttributeError`. `Update` discards them with `_`. Today the schema's `stringvalidator.OneOf` makes the path unreachable, but discarded errors are a hazard for future relaxations.

There is no behavior change to test (the path is unreachable today); this is a correctness-by-symmetry fix. Skip TDD here.

- [ ] **Step 2.1: Replace `Update`'s mode parsing**

In `internal/provider/security_group_resource.go`, replace lines 214-225 (the `normalizeMode` + `buildRules` block at the top of `Update`) with:

```go
ingressMode, err := normalizeMode(plan.IngressMode)
if err != nil {
    resp.Diagnostics.AddAttributeError(path.Root("ingress_mode"), "Invalid ingress_mode", err.Error())
    return
}
egressMode, err := normalizeMode(plan.EgressMode)
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
```

This mirrors `Create` (lines 125-145) verbatim.

- [ ] **Step 2.2: Confirm the SG suite still passes**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run SecurityGroup -v'
```

Expected: all pass (no test should change behavior).

---

## Task 3: VM `Update` refreshes state on partial-progress error

**Files:**
- Modify: `internal/client/mock/vm.go` (add a failure-injection knob)
- Modify: `internal/provider/vm_resource.go:267-309` (error-path refresh)
- Modify: `internal/provider/vm_resource_test.go` (regression test using the new knob)

If `AddVMStorages` succeeds but `RemoveVMStorages` fails, today we return an error before `GetVM`. State stays equal to the *plan*, which now disagrees with the *API truth*. On retry the user sees a stale picture.

Fix: refresh state from `GetVM` even on failure, then return the error. The user will see the correct partial state and can retry.

- [ ] **Step 3.1: Add a failure-injection knob to the VM mock**

Open `internal/client/mock/vm.go`. Find the `wireVMs` function. After the `s.mux.HandleFunc("/v2/vms", ...)` block (covering POST/GET on the list endpoint), the file already registers per-VM endpoints. Locate the `/storages/remove` handler (look for `storages/remove` in the file). If it doesn't exist as a separate handler, find the dispatcher that routes `/storages/(add|remove)`.

Add a struct field to `Server` for the knob. In `internal/client/mock/server.go`, inside the `Server` struct (after `vmWiring sync.Once` on roughly line 42), add:

```go
    // FailRemoveVMStorages, when set, makes /v2/vms/{id}/storages/remove return 500.
    FailRemoveVMStorages bool
```

Then in `internal/client/mock/vm.go`, locate the `/storages/remove` handler. At the start of its `case http.MethodPost` block, add:

```go
s.mu.Lock()
fail := s.FailRemoveVMStorages
s.mu.Unlock()
if fail {
    s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "injected: storages/remove failure"})
    return
}
```

(If the handler doesn't separate methods, add the same guard at the top of the handler — read the existing file before editing to match its idiom.)

- [ ] **Step 3.2: Add the regression test**

Open `internal/provider/vm_resource_test.go`. Add (or extend the existing test file with) a test that:

1. Creates a VM with a single data disk attached.
2. Plans an Update that simultaneously removes the original disk and adds a new one.
3. Sets the mock failure flag so `RemoveVMStorages` returns 500.
4. Asserts that the apply errored AND that state's `data_disk_ids` reflects API truth (the added disk is present, the removed disk is *also* still present because remove failed).

```go
func TestUnitVM_PartialUpdateRefreshesState(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	// First step creates a VM with one data disk; second step intentionally
	// fails the remove half of an add+remove update and asserts state is
	// refreshed from the API rather than left equal to the plan.
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: vmPartialUpdateBaseConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "1"),
				),
			},
			{
				PreConfig: func() { h.Mock.FailRemoveVMStorages = true },
				Config:    vmPartialUpdateRotateConfig(),
				ExpectError: regexp.MustCompile(`Detach VM storages failed`),
			},
			// Third step turns failure off and asserts state caught up.
			{
				PreConfig: func() { h.Mock.FailRemoveVMStorages = false },
				Config:    vmPartialUpdateRotateConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "1"),
				),
			},
		},
	})
}

func vmPartialUpdateBaseConfig() string {
	return `
resource "cloudless_storage" "first" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "first"
  storage_type = "NVME"
  volume_gb    = 10
  replicated   = false
}
resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaab"
  boot_disk { storage_id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaac" }
  data_disk_ids    = [cloudless_storage.first.id]
}
`
}

func vmPartialUpdateRotateConfig() string {
	return `
resource "cloudless_storage" "first" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "first"
  storage_type = "NVME"
  volume_gb    = 10
  replicated   = false
}
resource "cloudless_storage" "second" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "second"
  storage_type = "NVME"
  volume_gb    = 10
  replicated   = false
}
resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaab"
  boot_disk { storage_id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaac" }
  data_disk_ids    = [cloudless_storage.second.id]
}
`
}
```

Add `"regexp"` to the file's imports if not already present.

> NOTE: the exact configuration_id / boot_disk storage_id UUIDs above are stand-ins. If the mock requires specific seeded values, replace them with whatever `mock.New()` already seeds (read `internal/client/mock/server.go` for any `Seed*` helpers). If no seeding helper covers a VM configuration, add one in this task scoped to what the test needs.

- [ ] **Step 3.3: Run the test and confirm it fails (state stale after partial error)**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run TestUnitVM_PartialUpdateRefreshesState -v'
```

Expected: FAIL — second step errors, but the third step finds `data_disk_ids = [second.id]` even though the API still has `first.id` attached. Or, more likely, the second step itself fails on a state-vs-API mismatch detected by Terraform's own consistency check.

- [ ] **Step 3.4: Implement the error-path refresh in `vm_resource.go`**

In `internal/provider/vm_resource.go`, locate `Update` (currently lines 249-310). Refactor the error-handling around `AddVMStorages`/`RemoveVMStorages` so any failure refreshes state before returning:

```go
refreshAndSet := func() {
    got, err := r.c.GetVM(ctx, id)
    if err != nil {
        resp.Diagnostics.AddError("Read VM after partial update failed", err.Error())
        return
    }
    r.fill(&plan, got)
    resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

if len(toAdd) > 0 {
    if err := r.c.AddVMStorages(ctx, id, toAdd); err != nil {
        resp.Diagnostics.AddError("Attach VM storages failed", err.Error())
        refreshAndSet()
        return
    }
}
if len(toRemove) > 0 {
    if err := r.c.RemoveVMStorages(ctx, id, toRemove); err != nil {
        resp.Diagnostics.AddError("Detach VM storages failed", err.Error())
        refreshAndSet()
        return
    }
}
```

Replace the existing `if len(toAdd) > 0 { … }` and `if len(toRemove) > 0 { … }` blocks (lines 290-301) with the version above. The trailing `r.c.GetVM(ctx, id)` + `r.fill(&plan, got)` block (lines 303-309) stays — it handles the success path.

- [ ] **Step 3.5: Run the test and confirm it passes**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run TestUnitVM_PartialUpdateRefreshesState -v'
```

Expected: PASS.

- [ ] **Step 3.6: Run the full provider suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./...'
```

Expected: all green.

- [ ] **Step 3.7: Commit (Must Fix bundle)**

```bash
cd /home/ubuntu/projects/cloudless-terraform
git add internal/provider/ssh_key_resource.go \
        internal/provider/ssh_key_resource_test.go \
        internal/provider/security_group_resource.go \
        internal/provider/vm_resource.go \
        internal/provider/vm_resource_test.go \
        internal/client/mock/server.go \
        internal/client/mock/vm.go
git commit -m "$(cat <<'EOF'
fix: review must-fixes (ssh_key rename ForceNew, SG Update err handling, VM partial-update state refresh)

- cloudless_ssh_key.name now RequiresReplace; the Fluence API has no
  rename endpoint and the previous no-op Update produced a permanent
  diff on rename.
- security_group Update now surfaces normalizeMode errors via
  AddAttributeError, mirroring Create. The path is unreachable today
  thanks to schema validation, but symmetric error handling avoids a
  silent failure window if the validator is later relaxed.
- vm Update now refreshes state from GetVM when AddVMStorages or
  RemoveVMStorages partially fails, so subsequent plans see the
  actual API truth instead of the (incorrect) plan-equal state.
- Mock gains a FailRemoveVMStorages knob to drive the VM regression
  test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

# Commit 2 — Should Fix

## Task 4: Replace `client.asErr` with `errors.As`

**Files:**
- Modify: `internal/client/client.go:65-88`

The hand-rolled `asErr` shim doesn't handle multi-error trees and exists only "to avoid importing errors". Use the stdlib.

- [ ] **Step 4.1: Replace `IsNotFound` and delete `asErr`**

In `internal/client/client.go` lines 65-88, replace the entire block with:

```go
// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
    var ae *APIError
    if errors.As(err, &ae) {
        return ae.StatusCode == http.StatusNotFound
    }
    return false
}
```

Add `"errors"` to the imports at the top of the file (alphabetical order, stdlib block).

- [ ] **Step 4.2: Run the client tests**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/client/... -v'
```

Expected: all pass. If any test mocked `asErr` directly, update it to use `errors.As`.

- [ ] **Step 4.3: Run the full provider suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./...'
```

Expected: all green.

---

## Task 5: Rename mock-backed `TestAcc*` → `TestUnit*`

**Files:**
- Modify: every `internal/provider/*_resource_test.go` (mock-backed) and `internal/provider/*_data_source_test.go` (mock-backed) that uses `tfharness` and starts a function with `TestAcc`
- Modify: `internal/provider/validator_apply_test.go`

`TestAcc*` is reserved (per `README.md`) for `TF_ACC=1`-gated real-API tests. Mock tests using `resource.UnitTest` should not share that prefix.

- [ ] **Step 5.1: List the offending tests**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && grep -rn "func TestAcc" internal/provider/ | grep -v _acc_test.go'
```

Expected output: a list of every mock-backed test that needs renaming (matches the rule: file does not end in `_acc_test.go` AND function starts with `TestAcc`).

- [ ] **Step 5.2: Rename each function**

For every match from Step 5.1, replace the function name's `TestAcc` prefix with `TestUnit`. Example:

```go
// before
func TestAccSSHKey_CreateAndRead(t *testing.T) { ... }
// after
func TestUnitSSHKey_CreateAndRead(t *testing.T) { ... }
```

Use sed for the mechanical rewrite, scoped to non-acc files only:

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && \
  for f in $(grep -lr "func TestAcc" internal/provider/ | grep -v _acc_test.go); do \
    sed -i "s/func TestAcc/func TestUnit/g" "$f"; \
  done'
```

- [ ] **Step 5.3: Confirm `_acc_test.go` files are untouched**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && grep -rn "func TestUnit" internal/provider/*_acc_test.go || echo "no leakage"'
```

Expected: `no leakage`.

- [ ] **Step 5.4: Run the suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./...'
```

Expected: all green; the test count is unchanged.

---

## Task 6: Add `CheckDestroy` to every real-API acc test

**Files:**
- Create: `internal/provider/acctest/checks.go`
- Modify: every `internal/provider/*_acc_test.go`

The framework calls `Delete` after the last step, but `CheckDestroy` is what verifies it. Centralize one helper per resource type.

- [ ] **Step 6.1: Create `internal/provider/acctest/checks.go`**

```go
package acctest

import (
    "context"
    "fmt"

    "github.com/hashicorp/terraform-plugin-testing/terraform"

    "github.com/cloudless/terraform-provider-cloudless/internal/client"
)

// CheckDestroy returns a TestCheckFunc that asserts every state resource of
// type tfType has been deleted on the API side, using getByID to fetch.
//
// getByID should return *APIError 404 when the resource is gone; any other
// error is treated as a transient failure and surfaced. The first arg is the
// resource type as it appears in HCL (e.g. "cloudless_ssh_key").
func CheckDestroy(c *client.Client, tfType string, getByID func(ctx context.Context, id string) error) func(*terraform.State) error {
    return func(s *terraform.State) error {
        for _, rs := range s.RootModule().Resources {
            if rs.Type != tfType {
                continue
            }
            err := getByID(context.Background(), rs.Primary.ID)
            if err == nil {
                return fmt.Errorf("%s %s still exists", tfType, rs.Primary.ID)
            }
            if !client.IsNotFound(err) {
                return fmt.Errorf("%s %s: unexpected error during destroy check: %w", tfType, rs.Primary.ID, err)
            }
        }
        return nil
    }
}
```

- [ ] **Step 6.2: Wire it into each `*_acc_test.go`**

For each acc test, add a `CheckDestroy:` field to the `resource.TestCase`. Pattern (using `ssh_key_resource_acc_test.go` as the example):

```go
// at file level (or inline)
func sshKeyDestroy(c *client.Client) func(*terraform.State) error {
    return acctest.CheckDestroy(c, "cloudless_ssh_key", func(ctx context.Context, id string) error {
        _, err := c.GetSSHKey(ctx, id)
        return err
    })
}

// inside the TestCase
resource.Test(t, resource.TestCase{
    ProtoV6ProviderFactories: factories,
    CheckDestroy:             sshKeyDestroy(acctest.RealClient()),
    Steps: []resource.TestStep{ ... },
})
```

This requires `acctest.RealClient()` — a small helper that returns a `*client.Client` configured against the real API. Add it to `internal/provider/acctest/harness.go`:

```go
// RealClient returns a *client.Client pointed at the real Fluence API,
// authenticated via FLUENCE_API_KEY (which Setup already verified is set).
func RealClient() *client.Client {
    return client.New(os.Getenv("FLUENCE_ENDPOINT"), os.Getenv("FLUENCE_API_KEY"))
}
```

(Add `"github.com/cloudless/terraform-provider-cloudless/internal/client"` to the imports.)

Wire `CheckDestroy` into all nine acc tests:

| Test file | tfType | API call |
|---|---|---|
| `ssh_key_resource_acc_test.go` | `cloudless_ssh_key` | `c.GetSSHKey(ctx, id)` |
| `vpc_resource_acc_test.go` | `cloudless_vpc` | `c.GetVPC(ctx, id)` |
| `subnet_resource_acc_test.go` | `cloudless_subnet` | `c.GetSubnet(ctx, id)` |
| `security_group_resource_acc_test.go` | `cloudless_security_group` | `c.GetSecurityGroup(ctx, id)` |
| `storage_resource_acc_test.go` | `cloudless_storage` | `c.GetStorage(ctx, id)` |
| `public_ip_resource_acc_test.go` | `cloudless_public_ip` | `c.GetPublicIP(ctx, id)` |
| `vm_resource_acc_test.go` | `cloudless_vm` | `c.GetVM(ctx, id)` |
| `vm_public_ip_attachment_resource_acc_test.go` | `cloudless_vm_public_ip_attachment` | refresh-only — see below |
| `security_group_attachment_resource_acc_test.go` | `cloudless_security_group_attachment` | refresh-only — see below |

The two attachment resources don't have a "get by ID" — destroy means "the attachment is no longer recorded on the parent". For them, write a one-off CheckDestroy that fetches the parent (VM or interface) and asserts the binding is gone:

```go
func sgAttachmentDestroy(c *client.Client) func(*terraform.State) error {
    return func(s *terraform.State) error {
        for _, rs := range s.RootModule().Resources {
            if rs.Type != "cloudless_security_group_attachment" {
                continue
            }
            ifaces, err := c.ListVMInterfaces(context.Background(), rs.Primary.Attributes["vm_id"])
            if err != nil {
                if client.IsNotFound(err) {
                    continue
                }
                return err
            }
            for _, ni := range ifaces {
                if ni.ID == rs.Primary.Attributes["network_interface_id"] && ni.SecurityGroupID != nil {
                    return fmt.Errorf("interface %s still bound to SG %s", ni.ID, *ni.SecurityGroupID)
                }
            }
        }
        return nil
    }
}
```

(Mirror for `vm_public_ip_attachment` using `c.GetVM(ctx, vm_id).PublicIP`.)

- [ ] **Step 6.3: Compile-check (acc tests skip without TF_ACC=1)**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run TestAcc -v'
```

Expected: every test reports `--- SKIP` with reason `set TF_ACC=1 to run acceptance tests`. Compile failures here would surface as a `FAIL`.

---

## Task 7: `FindVMByInterface` — drive paging from server response

**Files:**
- Modify: `internal/client/client.go:504-532`

Replace the local page counter with the server-reported `CurrentPage` so the API contract is unambiguous.

- [ ] **Step 7.1: Replace the loop**

In `internal/client/client.go`, replace the body of `FindVMByInterface` (lines 504-532) with:

```go
func (c *Client) FindVMByInterface(ctx context.Context, interfaceID string) (*VM, error) {
    // The API doesn't expose an interface→vm filter. Walk the user's VMs in
    // pages, indexing pages 1..N as the server reports them.
    //
    // TODO(fluence-api): replace with /v2/vms?interfaces=<id> when the API
    // adds the filter — this scan is O(N) over the user's whole fleet.
    const maxIters = 10000 // ~2M VMs at per_page=200; defensive cap if pagination metadata never converges.
    nextPage := uint64(1)
    for iter := 0; iter < maxIters; iter++ {
        q := url.Values{"page": {FormatPage(nextPage)}, "per_page": {"200"}}
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
        if resp.Pagination.CurrentPage >= uint64(resp.Pagination.TotalPages) {
            return nil, &APIError{StatusCode: http.StatusNotFound, Message: "no VM owns interface " + interfaceID}
        }
        nextPage = resp.Pagination.CurrentPage + 1
    }
    return nil, fmt.Errorf("FindVMByInterface: pagination did not terminate after %d iterations", maxIters)
}
```

- [ ] **Step 7.2: Run client tests**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/client/... ./internal/provider -run "VM|Attachment" -v'
```

Expected: all pass. If the mock's `/v2/vms` listing returns `CurrentPage=0`, the loop will exit immediately on iteration 1 — that's fine for the no-match path, but make sure mock-backed attachment tests still find their VMs (which they should because they only seed one page).

---

## Task 8: `NewWithClient` takes a version

**Files:**
- Modify: `internal/provider/provider.go:30-32`
- Modify: `internal/provider/testing/harness.go`

Stop hardcoding `version: "test"`.

- [ ] **Step 8.1: Update the signature**

In `internal/provider/provider.go`, replace `NewWithClient` (lines 30-32) with:

```go
// NewWithClient is used by unit tests to inject a pre-built client (typically
// pointed at a mock HTTP server). The returned provider skips the api_key
// resolution and uses the supplied client for every resource and data source.
func NewWithClient(c *client.Client, version string) func() provider.Provider {
    return func() provider.Provider { return &cloudlessProvider{version: version, overrideClient: c} }
}
```

- [ ] **Step 8.2: Update the harness**

In `internal/provider/testing/harness.go`, replace the `provider.NewWithClient(c)()` call with `provider.NewWithClient(c, "unit-test")()`.

- [ ] **Step 8.3: Run the suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./...'
```

Expected: all green.

---

## Task 9: Replace `math/rand` with `acctest.RandStringFromCharSet` in acc tests

**Files:**
- Modify: every `internal/provider/*_acc_test.go`

`Int63()` collisions across parallel runs are unlikely but possible; the test-patterns skill recommends `RandStringFromCharSet`.

- [ ] **Step 9.1: Replace import + caller per file**

For every `_acc_test.go`, swap:

```go
import "math/rand"
// ...
name := fmt.Sprintf("tf-acc-vpc-%d", rand.Int63())
```

with:

```go
import "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
// ...
name := "tf-acc-vpc-" + acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
```

(Note the import package `acctest` is from terraform-plugin-testing — NOT our internal `acctest`. Our local import is aliased; if a file imports both, alias the external as `tfacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"` and call `tfacctest.RandStringFromCharSet`.)

- [ ] **Step 9.2: Compile-check**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./...'
```

Expected: clean build.

---

## Task 10: Switch mock tests from `UnitTest` → `ParallelTest`

**Files:**
- Modify: every mock-backed test that uses `resource.UnitTest`

`ParallelTest` is the modern default per the test-patterns skill. Mock-backed tests have no shared state.

- [ ] **Step 10.1: Mechanical rewrite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && \
  for f in $(grep -lr "resource.UnitTest" internal/provider/ | grep -v _acc_test.go); do \
    sed -i "s/resource.UnitTest(/resource.ParallelTest(/g" "$f"; \
  done'
```

- [ ] **Step 10.2: Run the suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./...'
```

Expected: all green; total wall-clock should drop noticeably.

- [ ] **Step 10.3: Commit (Should Fix bundle)**

```bash
cd /home/ubuntu/projects/cloudless-terraform
git add internal/client/client.go \
        internal/provider/provider.go \
        internal/provider/testing/harness.go \
        internal/provider/acctest/harness.go \
        internal/provider/acctest/checks.go \
        internal/provider/*_test.go \
        internal/provider/*_acc_test.go
git commit -m "$(cat <<'EOF'
chore: review should-fixes (errors.As, test naming, CheckDestroy, paging, version param, RNG, ParallelTest)

- client: replace hand-rolled asErr with errors.As (handles multi-error
  trees, smaller surface).
- tests: rename mock-backed TestAcc* → TestUnit*; reserve TestAcc* for
  the TF_ACC=1-gated real-API tests.
- acc tests: every TestCase now sets CheckDestroy to verify resources
  are actually gone after Delete.
- client.FindVMByInterface: drive paging from server-reported
  CurrentPage so the 0/1-based ambiguity is gone.
- provider.NewWithClient takes an explicit version; "test" sentinel
  retired.
- acc tests: math/rand.Int63 swapped for acctest.RandStringFromCharSet.
- mock tests: resource.UnitTest → resource.ParallelTest where
  applicable.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

# Commit 3 — Nits

## Task 11: Delete dead `notFoundOrRemoved` helper

**Files:**
- Modify: `internal/provider/util.go:185-190`

- [ ] **Step 11.1: Delete the function**

In `internal/provider/util.go`, delete lines 185-190 (the `notFoundOrRemoved` definition and its leading comment).

- [ ] **Step 11.2: Compile-check**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./...'
```

Expected: clean — confirmed dead.

---

## Task 12: Inline single-call `pathRoot` wrapper

**Files:**
- Modify: `internal/provider/util.go:48-49`
- Modify: `internal/provider/provider.go:80`

- [ ] **Step 12.1: Update the call site**

In `internal/provider/provider.go` line 80, replace `pathRoot("api_key")` with `path.Root("api_key")`. Add `"github.com/hashicorp/terraform-plugin-framework/path"` to the imports.

- [ ] **Step 12.2: Delete the wrapper**

In `internal/provider/util.go`, delete the `pathRoot` function (lines 48-49) and its preceding comment line. Remove the now-unused `path` import if no other helpers use it (search the file).

- [ ] **Step 12.3: Compile-check**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go build ./...'
```

Expected: clean.

---

## Task 13: Drop unused initial allocations in storage/vpc Update

**Files:**
- Modify: `internal/provider/storage_resource.go:168`
- Modify: `internal/provider/vpc_resource.go:158`

- [ ] **Step 13.1: Replace `out := &client.Storage{}` with `var out *client.Storage`**

In `internal/provider/storage_resource.go` line 168, replace:

```go
out := &client.Storage{}
```

with:

```go
var out *client.Storage
```

- [ ] **Step 13.2: Same fix in vpc_resource.go**

In `internal/provider/vpc_resource.go` line 158, replace:

```go
out := &client.VPC{}
```

with:

```go
var out *client.VPC
```

- [ ] **Step 13.3: Run the suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./...'
```

Expected: all green (behavior identical — both branches assign before use).

---

## Task 14: Migrate one mock test to `ConfigStateChecks` as a reference

**Files:**
- Modify: `internal/provider/ssh_key_resource_test.go`

Demonstrate the modern pattern from `provider-test-patterns`. Future tests can copy this. We don't migrate everything — that's a separate effort.

- [ ] **Step 14.1: Rewrite `TestUnitSSHKey_CreateAndRead` to use ConfigStateChecks**

Replace the existing function body in `internal/provider/ssh_key_resource_test.go`:

```go
func TestUnitSSHKey_CreateAndRead(t *testing.T) {
    h := tfharness.New()
    defer h.Close()

    resource.ParallelTest(t, resource.TestCase{
        ProtoV6ProviderFactories: h.Factories,
        Steps: []resource.TestStep{
            {
                Config: `
resource "cloudless_ssh_key" "me" {
  name       = "demo"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1DjqOOxINs78oU3f7PJXIyq9uiNocNVhXNx user@example.com"
}
`,
                ConfigStateChecks: []statecheck.StateCheck{
                    statecheck.ExpectKnownValue("cloudless_ssh_key.me", tfjsonpath.New("name"), knownvalue.StringExact("demo")),
                    statecheck.ExpectKnownValue("cloudless_ssh_key.me", tfjsonpath.New("id"), knownvalue.NotNull()),
                    statecheck.ExpectKnownValue("cloudless_ssh_key.me", tfjsonpath.New("fingerprint"), knownvalue.NotNull()),
                },
            },
        },
    })
}
```

Imports needed:

```go
"github.com/hashicorp/terraform-plugin-testing/knownvalue"
"github.com/hashicorp/terraform-plugin-testing/statecheck"
"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
```

- [ ] **Step 14.2: Run the test**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run TestUnitSSHKey_CreateAndRead -v'
```

Expected: PASS.

---

## Task 15: SG `Update` — only PATCH ingress/egress when they actually changed

**Files:**
- Modify: `internal/provider/security_group_resource.go:206-243`

Today every Update sends both rule lists, even on a name-only change. Trim to the diff.

- [ ] **Step 15.1: Track changes locally**

Replace the body of `Update` (after the `req.Plan.Get` / `req.State.Get` and `normalizeMode` blocks already fixed in Task 2) with:

```go
upd := client.UpdateSecurityGroupRequest{}
if !plan.Name.Equal(state.Name) {
    n := plan.Name.ValueString()
    upd.Name = &n
}

stateIngressMode := apiToMode(client.SecurityGroupRules{Mode: stateModeWire(state.IngressMode)})
stateEgressMode := apiToMode(client.SecurityGroupRules{Mode: stateModeWire(state.EgressMode)})
if ingressMode != stateIngressMode || !rulesEqual(plan.Ingress, state.Ingress) {
    upd.IngressRules = &ingress
}
if egressMode != stateEgressMode || !rulesEqual(plan.Egress, state.Egress) {
    upd.EgressRules = &egress
}

out, err := r.c.UpdateSecurityGroup(ctx, state.ID.ValueString(), upd)
if err != nil {
    resp.Diagnostics.AddError("Update SG failed", err.Error())
    return
}
r.fill(&plan, out, ingressMode, egressMode)
resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
```

Add `rulesEqual` and `stateModeWire` to `security_group_translate.go`:

```go
// rulesEqual reports whether two []sgRule slices are semantically equal.
func rulesEqual(a, b []sgRule) bool {
    if len(a) != len(b) {
        return false
    }
    for i := range a {
        if !a[i].Protocol.Equal(b[i].Protocol) ||
            !a[i].Ports.Equal(b[i].Ports) ||
            !a[i].CIDR.Equal(b[i].CIDR) ||
            !a[i].SecurityGroupID.Equal(b[i].SecurityGroupID) ||
            !a[i].Type.Equal(b[i].Type) {
            return false
        }
    }
    return true
}

// stateModeWire flips an HCL-mode string back to the wire form so apiToMode
// can re-classify it for diff purposes.
func stateModeWire(m types.String) string {
    switch m.ValueString() {
    case "allow_all":
        return "allowAll"
    case "deny_all", "allow_listed":
        return "allow"
    default:
        return "allowAll"
    }
}
```

- [ ] **Step 15.2: Run the SG suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./internal/provider -run SecurityGroup -v'
```

Expected: all pass. The mock doesn't currently observe how many fields each PATCH carries, so behavior tests stay green; if you want to assert the optimization, extend the mock to record last-PATCHed fields and add an assertion — out of scope for this nit task.

---

## Task 16: Promote anonymous `dataDisks` body to a named type

**Files:**
- Modify: `internal/client/client.go:451-462`

Three call sites build the same anonymous struct. One is enough.

- [ ] **Step 16.1: Add the named type and reuse**

In `internal/client/client.go`, just before `AddVMStorages` (line ~451), add:

```go
type vmStoragesBody struct {
    DataDisks []string `json:"dataDisks"`
}
```

Then rewrite `AddVMStorages` and `RemoveVMStorages`:

```go
func (c *Client) AddVMStorages(ctx context.Context, vmID string, storageIDs []string) error {
    return c.do(ctx, http.MethodPost, "/v2/vms/"+vmID+"/storages/add", nil, vmStoragesBody{DataDisks: storageIDs}, nil)
}

func (c *Client) RemoveVMStorages(ctx context.Context, vmID string, storageIDs []string) error {
    return c.do(ctx, http.MethodPost, "/v2/vms/"+vmID+"/storages/remove", nil, vmStoragesBody{DataDisks: storageIDs}, nil)
}
```

- [ ] **Step 16.2: Compile + test**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./...'
```

Expected: all green.

---

## Task 17: Add `tflog.Debug` breadcrumb in `waitFor`

**Files:**
- Modify: `internal/provider/util.go` (the `waitFor` function)

Surface poll progress under `TF_LOG=DEBUG` without spamming Info.

- [ ] **Step 17.1: Add the import**

At the top of `internal/provider/util.go`, add:

```go
"github.com/hashicorp/terraform-plugin-log/tflog"
```

- [ ] **Step 17.2: Log each iteration**

In `waitFor`, after `err := fn(ctx)` and before the error-handling block, add:

```go
tflog.Debug(ctx, "waitFor iteration", map[string]any{
    "interval_ms": opts.Interval.Milliseconds(),
    "remaining":   time.Until(deadline).Truncate(time.Second).String(),
})
```

(If `errStopPolling` is hit immediately the log fires once before exit — that's intentional; it confirms the poll ran at least once.)

- [ ] **Step 17.3: Add tflog as a direct dependency if needed**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go mod tidy'
```

Confirm `terraform-plugin-log` moves out of `// indirect`. If it doesn't (already direct), `tidy` is a no-op.

- [ ] **Step 17.4: Run the suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && go test ./...'
```

Expected: all green.

- [ ] **Step 17.5: Commit (Nits bundle)**

```bash
cd /home/ubuntu/projects/cloudless-terraform
git add internal/provider/util.go \
        internal/provider/provider.go \
        internal/provider/storage_resource.go \
        internal/provider/vpc_resource.go \
        internal/provider/security_group_resource.go \
        internal/provider/security_group_translate.go \
        internal/provider/ssh_key_resource_test.go \
        internal/client/client.go \
        go.mod go.sum
git commit -m "$(cat <<'EOF'
chore: review nits (dead helpers, allocations, ConfigStateChecks demo, SG Update diff, named bodies, tflog)

- delete unused notFoundOrRemoved and the single-use pathRoot wrapper.
- drop unused initial &client.{Storage,VPC}{} allocations in Update.
- migrate ssh_key_resource_test.go's CreateAndRead to ConfigStateChecks
  as a reference for future tests.
- security_group Update only sends ingress/egress when they actually
  changed, instead of unconditionally re-PATCHing both lists.
- promote the anonymous {DataDisks} request body to a named type
  reused by AddVMStorages and RemoveVMStorages.
- add tflog.Debug breadcrumb in waitFor so TF_LOG=DEBUG surfaces each
  poll iteration's remaining timeout.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

- [ ] **Step F.1: Full suite + vet + fmt**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && \
  gofmt -l . | tee /tmp/fmt.out && [ ! -s /tmp/fmt.out ] && \
  go vet ./... && \
  go test ./...'
```

Expected: empty fmt list, vet silent, tests green.

- [ ] **Step F.2: Confirm 3 commits since branch base**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform && git log --oneline master..HEAD'
```

Expected: exactly three commits with subjects:
1. `fix: review must-fixes …`
2. `chore: review should-fixes …`
3. `chore: review nits …`
