# Cloudless Provider — Phase 3 Hygiene Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply five small hygiene fixes catalogued during Phase 2's final review, leaving the provider's quality and test coverage measurably better without any user-visible behavior change.

**Architecture:** Each task is a focused, near-mechanical change touching 1-3 files. No new packages, no schema migrations. Carry-forwards from prior reviews are folded into a single hardening pass that ships in five commits.

**Tech Stack:** Go 1.25 (existing), terraform-plugin-framework v1.19, GitHub Actions (existing), tfplugindocs (existing).

---

## Decomposition note

This is a hygiene-only phase. Out of scope: first release tag, module path rename, registry namespace, GPG-signed artifact publish, anything publish-mechanics. Those land in their own milestone whenever a registry namespace is decided.

The five tasks are independent — they could land in any order. I order them by user-visible impact: CI guardrails first (so future changes don't drift), then the doc completeness item (visible in the registry preview), then test improvements.

## File structure overview

After Phase 3:

```
.
├── .github/workflows/build.yml                        # MODIFY: add docs-check + gofmt-check jobs
├── docs/resources/security_group.md                   # REGENERATED (nested rule descriptions)
├── internal/provider/security_group_resource.go       # MODIFY: add Description to sgRuleBlock attrs
├── internal/provider/{vm,vm_public_ip_attachment,security_group_attachment}_resource_acc_test.go
│                                                      # MODIFY: pick OS image by slug, not images[0]
├── internal/provider/{vpc,security_group,storage,public_ip}_resource_acc_test.go
│                                                      # MODIFY: add rename/resize Update step
└── internal/provider/validators/uuid_e2e_test.go      # CREATE: end-to-end UUID rejection
```

## How to read this plan

Each task is a single logical change with one commit at the end. Steps are 2-5 minute actions. Run all Go commands inside `nix develop`. Pattern: `nix develop --command bash -c '...'`.

The 25 unit tests + 9 SKIP acc tests baseline must hold across every step. After Task 5, total count rises to 26 unit tests (one new validator e2e).

---

## Task 1: CI hygiene — docs-stale + gofmt checks

**Files:**
- Modify: `.github/workflows/build.yml`

The current CI runs `go build`, `go vet`, `go test`. Two gaps surfaced in Phase 2's final review:
- Schema description changes can land without re-generating `docs/`. Operators reading the registry would see stale descriptions.
- `gofmt` drift can sneak in (Phase 2 had to fix `vm_resource.go` after the fact).

Add two CI steps. Both are zero-config and complete in under 30 seconds on a 1.25-baseline runner.

- [ ] **Step 1.1: Modify `.github/workflows/build.yml`**

Open the file and add two new steps after the existing `Test` step:

```yaml
      - name: gofmt check
        run: |
          out=$(gofmt -l .)
          if [ -n "$out" ]; then
            echo "Files need gofmt:"
            echo "$out"
            exit 1
          fi

      - name: docs stale check
        run: |
          go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name cloudless
          if ! git diff --exit-code docs/; then
            echo "docs/ is stale — run 'make docs' and commit the result"
            exit 1
          fi
```

The `docs stale check` deliberately uses `go run` instead of relying on `tfplugindocs` being on PATH (the dev shell has it via nix; CI doesn't). The `tools.go` import already pins the version.

- [ ] **Step 1.2: Verify CI workflow syntax locally**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && cat .github/workflows/build.yml'
```
Confirm the new steps are present and indented under `steps:`.

- [ ] **Step 1.3: Verify both checks would pass on the current tree**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && gofmt -l . | grep -v "^$" || echo "gofmt clean"'
```
Expected: `gofmt clean`. (If anything is listed, run `gofmt -w` on it before proceeding.)

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && tfplugindocs generate --provider-name cloudless && git diff --exit-code docs/'
```
Expected: silent, exit 0. (If non-zero, the docs were stale before Task 1; investigate and regenerate.)

- [ ] **Step 1.4: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene
git add .github/workflows/build.yml
git commit -m "$(cat <<'EOF'
ci: add gofmt + docs-stale checks

gofmt -l . fails the build if any file isn't formatted (Phase 2 had
to fix vm_resource.go after the fact).
tfplugindocs generate + git diff catches schema-description changes
that didn't trigger a docs regeneration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: SG nested rule attribute descriptions

**Files:**
- Modify: `internal/provider/security_group_resource.go` (`sgRuleBlock()` function)
- Regenerate: `docs/resources/security_group.md`

Phase 2 added top-level `Description` to `ingress_mode` and `egress_mode`, but the nested `sgRuleBlock()` attributes (`protocol`, `ports`, `cidr`, `security_group_id`, `type`) have no descriptions. The generated `docs/resources/security_group.md` therefore lists them as bare types. Add per-attribute descriptions and regenerate.

- [ ] **Step 2.1: Find `sgRuleBlock()` and inspect**

```bash
grep -n 'func sgRuleBlock' /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene/internal/provider/security_group_resource.go
```

Open the function. It currently looks like (line numbers approximate):

```go
func sgRuleBlock() schema.NestedBlockObject {
	return schema.NestedBlockObject{
		Attributes: map[string]schema.Attribute{
			"protocol":          schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("tcp", "udp", "icmp", "all")}},
			"ports":             schema.StringAttribute{Optional: true, Validators: []validator.String{validators.PortSpec()}},
			"cidr":              schema.StringAttribute{Optional: true, Validators: []validator.String{validators.CIDR("any")}},
			"security_group_id": schema.StringAttribute{Optional: true, Validators: []validator.String{validators.UUID()}},
			"type":              schema.StringAttribute{Optional: true, Computed: true, Validators: []validator.String{stringvalidator.OneOf("ipv4", "ipv6")}},
		},
	}
}
```

- [ ] **Step 2.2: Add descriptions**

Replace the function body with the same structure but add `Description:` to each attribute. The exact text:

```go
func sgRuleBlock() schema.NestedBlockObject {
	return schema.NestedBlockObject{
		Attributes: map[string]schema.Attribute{
			"protocol": schema.StringAttribute{
				Required:    true,
				Description: "One of `tcp`, `udp`, `icmp`, `all`.",
				Validators:  []validator.String{stringvalidator.OneOf("tcp", "udp", "icmp", "all")},
			},
			"ports": schema.StringAttribute{
				Optional:    true,
				Description: "Single port (e.g. `443`), inclusive range (e.g. `8000-8100`), or `all`. Required for tcp/udp; must be empty for icmp/all.",
				Validators:  []validator.String{validators.PortSpec()},
			},
			"cidr": schema.StringAttribute{
				Optional:    true,
				Description: "Remote address as a CIDR block (e.g. `10.0.0.0/24`). Mutually exclusive with `security_group_id`.",
				Validators:  []validator.String{validators.CIDR("any")},
			},
			"security_group_id": schema.StringAttribute{
				Optional:    true,
				Description: "Remote security group's UUID — match traffic from any interface bound to this SG. Mutually exclusive with `cidr`.",
				Validators:  []validator.String{validators.UUID()},
			},
			"type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Address family: `ipv4` (default) or `ipv6`.",
				Validators:  []validator.String{stringvalidator.OneOf("ipv4", "ipv6")},
			},
		},
	}
}
```

- [ ] **Step 2.3: Regenerate docs**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && tfplugindocs generate --provider-name cloudless'
```
Expected: ends with the rendering log lines, exit 0.

- [ ] **Step 2.4: Confirm the descriptions show up in generated docs**

```bash
grep -A1 'protocol\|^- ports\|^- cidr\|security_group_id\|^- type' /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene/docs/resources/security_group.md | head -25
```
Expected: each attribute now has its description text on the same line.

- [ ] **Step 2.5: Run the suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && go build ./... && go vet ./... && go test ./... -count=1 -race 2>&1 | tail -8'
```
Expected: all 25 tests pass + 9 SKIP, build/vet clean.

- [ ] **Step 2.6: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene
git add internal/provider/security_group_resource.go docs/resources/security_group.md
git commit -m "$(cat <<'EOF'
docs(security_group): add descriptions to nested rule attributes

The protocol/ports/cidr/security_group_id/type attributes inside
ingress and egress blocks had no Description, so the generated docs
listed them as bare types. Now each carries a short, operator-facing
description with the format and constraints inline.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Acc-test image picker by slug

**Files:**
- Modify: `internal/provider/vm_resource_acc_test.go`
- Modify: `internal/provider/vm_public_ip_attachment_resource_acc_test.go`
- Modify: `internal/provider/security_group_attachment_resource_acc_test.go`

Three acc tests use `data.cloudless_default_images.all.images[0].download_url` for the boot disk. If the API ever returns a non-bootable or arch-mismatched image at index 0, those tests become flaky. Pick by slug instead — every existing example uses `ubuntu-24-04-x64`.

- [ ] **Step 3.1: Modify `vm_resource_acc_test.go`**

Find the `os_image = data.cloudless_default_images.all.images[0].download_url` line (likely around line 36-38 in the storage block of the test config).

Replace with:

```hcl
  os_image     = [for i in data.cloudless_default_images.all.images : i.download_url if i.slug == "ubuntu-24-04-x64"][0]
```

- [ ] **Step 3.2: Modify `vm_public_ip_attachment_resource_acc_test.go`**

Same change in the `cloudless_storage "boot"` block.

- [ ] **Step 3.3: Modify `security_group_attachment_resource_acc_test.go`**

Same change in the `cloudless_storage "boot"` block.

- [ ] **Step 3.4: Verify the suite still passes (acc tests still SKIP cleanly)**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && go build ./... && go vet ./... && go test ./... -count=1 2>&1 | tail -8'
```
Expected: 25 PASS + 9 SKIP. The acc tests don't run without `TF_ACC=1`, but the HCL-parsing happens at test time so any syntax error in the change would surface as a SKIP-with-error rather than a clean SKIP.

- [ ] **Step 3.5: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene
git add internal/provider/vm_resource_acc_test.go internal/provider/vm_public_ip_attachment_resource_acc_test.go internal/provider/security_group_attachment_resource_acc_test.go
git commit -m "$(cat <<'EOF'
test(acc): pick OS image by slug instead of images[0]

Indexing the first default image was fragile — if Fluence ever
reorders or adds a non-bootable image, the VM acc tests would silently
break. Filter by slug == "ubuntu-24-04-x64" to match the pattern
already used in examples/main.tf.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Update-path acc test coverage

**Files:**
- Modify: `internal/provider/vpc_resource_acc_test.go` (already covers rename — verify, no change needed unless missing)
- Modify: `internal/provider/security_group_resource_acc_test.go` (add rename step)
- Modify: `internal/provider/storage_resource_acc_test.go` (already covers resize per Phase 2 — verify)
- Modify: `internal/provider/public_ip_resource_acc_test.go` (add rename step)

Phase 2 created acc tests with one happy-path step each. The Update path (in-place mutation) is the most fragile and least covered. Add a second TestStep to each acc test that has an in-place updatable attribute, exercising the rename / resize flow.

The four resources with in-place updatable attributes are: VPC (name + enable_external), SG (name + rules), Storage (name + volume_gb), Public IP (name).

- [ ] **Step 4.1: Verify VPC acc test already covers rename**

```bash
grep -A2 'cloudless_vpc' /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene/internal/provider/vpc_resource_acc_test.go | head -30
```

If the test already has 2 steps (create + rename), do nothing. If only 1 step, add a second:

```hcl
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" { region = "DE" }

resource "cloudless_vpc" "main" {
  cluster_id = data.cloudless_cluster.main.id
  name       = %q
}
`, vpcName+"-renamed"),
				Check: resource.TestCheckResourceAttr("cloudless_vpc.main", "name", vpcName+"-renamed"),
```

(If the test was already 2-step, skip this sub-step.)

- [ ] **Step 4.2: Verify Storage acc test already covers resize**

```bash
grep -B1 -A3 'volume_gb' /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene/internal/provider/storage_resource_acc_test.go
```

Phase 2's plan said the storage acc test should exercise `volume_gb 100→200`. Confirm there are 2 TestStep entries with different `volume_gb` values. If only 1, add the second step.

- [ ] **Step 4.3: Add rename step to Public IP acc test**

Open `internal/provider/public_ip_resource_acc_test.go`. The current test has one TestStep. Add a second:

```go
			{
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" { region = "DE" }

resource "cloudless_public_ip" "edge" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = %q
  address_type = "V4"
}
`, pipName+"-renamed"),
				Check: resource.TestCheckResourceAttr("cloudless_public_ip.edge", "name", pipName+"-renamed"),
			},
```

(Place after the existing first TestStep, before the Import step if there is one.)

- [ ] **Step 4.4: Add rename step to SG acc test**

Open `internal/provider/security_group_resource_acc_test.go`. Add a second TestStep that renames the SG and keeps the same ingress rule. The pattern is the same as steps 4.1 and 4.3 — duplicate the create config, change the resource's `name` attribute to `sgName+"-renamed"`, assert the new name.

```go
			{
				Config: fmt.Sprintf(`
data "cloudless_cluster" "main" { region = "DE" }

resource "cloudless_security_group" "web" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = %q
  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
}
`, sgName+"-renamed"),
				Check: resource.TestCheckResourceAttr("cloudless_security_group.web", "name", sgName+"-renamed"),
			},
```

- [ ] **Step 4.5: Verify the suite still passes (acc tests still SKIP cleanly)**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && go build ./... && go vet ./... && go test ./... -count=1 2>&1 | tail -8'
```
Expected: 25 PASS + 9 SKIP, no compile errors.

- [ ] **Step 4.6: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene
git add internal/provider/vpc_resource_acc_test.go internal/provider/security_group_resource_acc_test.go internal/provider/storage_resource_acc_test.go internal/provider/public_ip_resource_acc_test.go
git commit -m "$(cat <<'EOF'
test(acc): add Update-path coverage to in-place mutable resources

Each acc test for VPC / SG / Storage / Public IP had only a create
step. The Update path is the most fragile (PATCH wire shape, server-
side rejection of immutable fields, etc.) and was untested by acc.
Each test now has a second step exercising the in-place update —
rename for VPC / SG / Public IP; resize for Storage (already had
this).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: End-to-end validator rejection test

**Files:**
- Create: `internal/provider/validators/uuid_e2e_test.go` (or `internal/provider/validator_apply_test.go` — pick the file scope below)

Phase 2 added validators and unit-tested them in isolation. There's no test confirming the validators are actually wired into resource schemas — i.e., that an invalid UUID in HCL produces a plan-time error, not a Create-time API failure.

This task adds one end-to-end test: a `cloudless_vpc` config with an invalid `cluster_id` should fail at plan with the validator's "Invalid UUID" message.

The test lives in `internal/provider/` (the provider's _test package), not in `internal/provider/validators/` — it exercises the validator-resource integration, not the validator in isolation.

- [ ] **Step 5.1: Create the test file**

Create `internal/provider/validator_apply_test.go`:

```go
package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

// TestAccValidator_RejectsInvalidUUID confirms the UUID validator wired into
// vpc.cluster_id actually fires at plan time, not just in unit-test isolation.
// Plan-time rejection means the API never sees the bad value.
func TestAccValidator_RejectsInvalidUUID(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_vpc" "broken" {
  cluster_id = "not-a-uuid"
  name       = "broken"
}
`,
				ExpectError: regexp.MustCompile(`Invalid UUID`),
			},
		},
	})
}
```

- [ ] **Step 5.2: Run the test**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && go test ./internal/provider/ -run TestAccValidator_RejectsInvalidUUID -v'
```
Expected: PASS.

If it fails, inspect the actual diagnostic message — the UUID validator's text is `"Invalid UUID"` per `validators/uuid.go`, but if the resource's plan-time error reports differently, adjust the regex to match.

- [ ] **Step 5.3: Run the full suite**

```bash
nix develop --command bash -c 'cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene && go build ./... && go vet ./... && go test ./... -count=1 -race 2>&1 | tail -8'
```
Expected: 26 PASS (25 prior + 1 new) + 9 SKIP, build/vet/race clean.

- [ ] **Step 5.4: Commit**

```bash
cd /home/ubuntu/projects/cloudless-terraform/.worktrees/phase3-hygiene
git add internal/provider/validator_apply_test.go
git commit -m "$(cat <<'EOF'
test: confirm UUID validator fires at plan time on a real resource

Phase 2 added validators and unit-tested them in isolation. This
test asserts the validator is actually wired into the schema by
giving cloudless_vpc.cluster_id a bogus value and asserting the
plan errors with "Invalid UUID" — i.e., the API never sees it.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Spec coverage check

After completing all 5 tasks, verify against the Phase 3 hygiene scope agreed during brainstorming:

| Hygiene item | Implemented in |
|---|---|
| CI: docs-stale check | Task 1 |
| CI: gofmt linter | Task 1 |
| SG nested rule attribute descriptions | Task 2 |
| Acc-test image picker by slug | Task 3 |
| Update-path acc test coverage (modest) | Task 4 |
| Validator end-to-end rejection test | Task 5 |

Out-of-scope (intentional, deferred to publish-prep milestone):
- First release tag, GPG-signed publication
- Module path rename
- `examples/resources/<name>/import.sh` files for Import sections in docs
- GitHub Actions release workflow on tag push
- Boot disk Read full population (would let `ImportStateVerifyIgnore: ["boot_disk"]` go away)
- Mock POST/PATCH decode-error surface (currently silent)

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-09-cloudless-provider-phase3-hygiene.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
