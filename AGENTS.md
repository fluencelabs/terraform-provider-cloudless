# `terraform-provider-cloudless`
Terraform provider for the Fluence cloud (vodopad public API at api.fluence.dev), for Fluence users who describe VMs, disks, networks, keys and security groups as HCL.

## What this project is
- **Nature**: `pre-release` — becomes `production` at the first Registry release. No working principle is relaxed: full production discipline applies now, so the release does not change how you work.
- **Graph**: `@cloudless/fluence` — every session starts with `iskron_orient` there.
- **Focus holon**: `#1787 «🧩 Контур terraform-provider-cloudless»` (under `#61` VM/Compute).
- **Agent role**: `#1789 «🤖 Агент репозитория terraform-provider-cloudless»` — adhikarin, steward of the focus holon. Your inbox: `iskron_orient(focus="1789")` at session start.
- **Owner role**: `#1788 «👤 Владелец terraform-provider-cloudless (Nick)»` (svatantra 主) — out-of-mandate questions go there as `posed_to` vimarshas.
- **Stack**: Go 1.25, terraform-plugin-framework, terraform-plugin-testing, tfplugindocs, goreleaser, golangci-lint v2.
- **Production statement**: ships to nobody yet (only the `edge` pre-release exists); after the first tag it ships to external Fluence users through Terraform Registry. Breakage then means a broken `terraform apply` at customers and orphaned paid VMs and disks that keep billing. Treat every change as if that release had happened.
- **Language**: everything committed to this repo — code, comments, docs, commit messages, PR text, this file — is written in English. Graph nodes follow the graph's language (Russian).

## Persistence rules
State lives in the **repo** or in the **graph** — nowhere else. The harness's built-in memory (per-project memory directory, conversation summaries, `/tmp`, machine-local files) is **forbidden entirely, not by category**: no project fact, no user preference, no working-style note goes there. (why: local memory is invisible to every other agent and machine, so it drifts silently and breaks the reproducibility that makes a second machine or agent possible.)
- **Repo**: code, configs, conventions, code gotchas, branch state — the artifact itself.
- **Graph**: methodology, design decisions, open questions (vimarshas), plans, handovers, lessons, hints — the thinking around it. Do not retell graph content in the repo; link the vimarsha or holon.
- **Retrieve state; never reconstruct from memory.** No source for "we decided…"? Stop and read the graph or repo before acting.
- **External design/spec files are drafts for intake**, not the record: the graph holds decisions; such a file is a view of them. In this tree, designs never go to `docs/`, `specs/` or scratch markdown — they go into the graph (see `~/projects/work/fluencelabs/CLAUDE.md`).
- **Parking lots are named, and there are three.** An undisciplined surface is free and a disciplined one costs, so under pressure laziness finds the parking lot: this file's prose; a sinn-phenomenon for something that acts; a lone `context` arrow that mutes the detector. The finish line of a write: **a node is not written until what pulls on it is named** — which kriya breaks if the node vanishes? None — that is parking, not a record.
- **This overrides the harness's own memory instruction**, which invites a `project` category and will keep inviting it. Route instead, always — and **before finishing, check that every long-lived fact from the user's context is persisted by this routing: an unpersisted fact is a task failure**, asking **whose fact is it?**
  Repo convention, code fact, this project's procedures, its servers and dated duties → this file / docs / code, or a node in this repo's graph; work state, decision, open question → a vimarsha in the graph. A dated duty carries its date in `attrs`. Project rules never land in another project's or the user's graph.
  Owner's routing decision: everything that outlives a session and is not a repo convention goes to the `@cloudless/fluence` graph — project facts under the focus holon, user-scoped facts as well; there is no separate personal graph for this work, and standing preferences live in this file or in the user's global instructions file, never in a node.
- The local memory directory is **evacuated and frozen**: `MEMORY.md` holds a one-line prohibition stub pointing here, and the memory-guard `PreToolUse` hook in `.claude/settings.json` blocks any write there (exit 2) the moment the save instinct fires.

## Session lifecycle
Graph = the work (structure, open questions, what next). Git = how we got here (SHAs, branches, PRs). **Git references do not enter the graph** — no SHAs, branch names, PR numbers or "shipped/merged" in nodes (they rot on rebase).
- **Session start:** enter via the `iskron:entry` skill — it ends in signs, not reading; the addresses are above (graph, focus holon, agent role, owner role). Address the owner by the role seq, not the `me` sentinel.
- **Starting work: graph first, then project, then code.** A substantive task enters in three beats: (1) **graph reconnaissance** — what is recorded about the place of change, which vimarshas are open, what is decided and rejected, **and what is recorded about external surfaces the work will touch** (see "External surfaces"); driven by `iskron:entry`; (2) **integration field** — from the focus holon, steward role and the change's nodes, derive consumers, effects, neighbouring holons and their actors via `iskron:integrity`; walk relays with `lens="trace"`, design missing kriyas and phenomena, weave gaps via `iskron:weaving`; (3) **design** the change (`iskron:design`) — only then code. The one exception is **explicit**: the human said "work right away" or named another protocol — then go to code and pay the reconnaissance debt at the end-of-task reconcile beat. Silence is not "work right away".
- **A decision is recorded when taken, not when executed.** Wherever it arrives — chat, socket, two agents agreeing — it stands in the graph **immediately**, with the modes it really has now: epistemic no higher than `anumita`, ontic `anagata`, volitive `chanda`/`adhimoksha`. Record who decided and what counts as done. (why: a decision left in the conversation dies with it.)
- **Every task is described before it is started — and recorded as what it is.** Before the first change outside the graph, the work stands in the graph by its carrier: a one-off act — an anga-vimarsha on the transformation it moves (a large one — its own bianhua); **a kriya only for a repeatable transition**. While the work runs, the graph moves with it. On **merge** (not push) modes switch to what the merge made true. Then **reality-audit**: check the claim against the deployed artifact, not the diff.
- **Every merge → update the graph.** A push that only opened or updated a PR shipped nothing. The post-merge sequence hangs on the **event** of the merge — never on a lull. When merged, every step below is mandatory:
  - **Reconcile with reality.** Record what positions the change in the target system: provider schema, client API surface, delivery, user experience. Pure repo mechanics (lockfile noise, internal refactors, file moves) stay in git. **Updating the graph means weaving, not editing prose.** Zero nodes and zero arrows after a substantive wave is an unperformed step — say so explicitly if there was truly nothing, and why.
  - **Advance the map.** Keep open work tied `anga` to the transformation it moves (the provider's v3 work drives `#967`). A thin `genre=hint` seed only for what the graph does not carry.
  - **Close along the axis, not by feeling "done".** Record the answer as `addressed_by`; release (`visarjana`) is a separate volitive act. Release yourself when three things meet: the answer stands in the graph as a node; the repo shows it; reality shows it as far as reachable — where unreachable, the user's word stands instead, and you asked.
  - **Sweep the shipped holon.** A push realizing designed nodes switches their modes (anagata→vartamana, kalpita→pratyakshita) across the *whole* designed holon and ends the design vimarshas the shipment resolved.
  - **Work the inbox.** `posed_to` questions the work answered end by the rule above; stale ones — park or group.
  - **Reconcile code and graph.** End of every substantive task — the `iskron:reconcile` beat: nodes vs code, code vs graph (a comment carrying meaning → graph, code keeps a reference), rejected alternatives recorded. Remaining debts as vimarshas, not narrative.
  - **Feedback reflection** — on merge, and at the close of a session no merge crowned: examine the session's experience *of the method itself* — where a skill, rule or surface failed, surprised, or worked for the wrong reason; check what is already recorded and write only a case worth recording, **by address**: into `@cloudless/fluence`, anchored on the tool's node or holon and posed to its steward (`posed_to="steward"`); method-general findings go to the owner role. Driven by the `iskron:feedback` skill. **An empty reflection is a valid outcome: zero records beat an opinion.**
  - **Vocabulary pass.** Re-read what you are about to land for borrowed project-management words (ticket, backlog, sprint, epic, story, done, blocker, committed). Do **not** substitute yourself: name each to the user and ask what it is called in the project.
- **Design completeness criterion:** a design is not *done* until its decisions, risks and lifecycle are in the graph. Working autonomously: land decisions and risks now; propose the transformation (bianhua) with a telos marked for owner confirmation.
- **Execution suites drive execution.** Planning, TDD, debugging, verification, review belong to the installed execution suite; the graph carries only the memory/design plane. Decisions and risks born in execution still land in the graph **before the session ends**.
- **A claim you made is not a claim you accept.** Behavioural claims — "the fix works", "the endpoint answers" — close on the verdict of the cold `verifier` sub-agent (`.claude/agents/verifier.md`), never on your re-read. Give it the claim, the carrier and the falsifier from *Reality* — and **wait for the verdict** before ending anything by the rule above.
- **Hook merging.** Where the harness has a hooks file, entries from different suites coexist — add beside, never overwrite.
- These reminders are automated in `.claude/settings.json`: session-start entry (`SessionStart`), post-`git push` graph ritual plus branch-freshness check (`PostToolUse` on Bash), memory-guard (`PreToolUse` on writes) and the no-freeze `Stop` hook. No spec-write hook: no workflow suite is installed, so no interop was agreed.
- **Keep this file honest.** It is generated by `iskron:iskronify` and stamped at the bottom with the contract it came from. Rerun `iskronify` when the installed skill's description names a higher contract (first word of its description) or when the sources this file derives from moved after the stamp: `git log -1 --format=%cd -- Makefile go.mod .github/workflows .golangci.yml` against the stamp date.
- **Keep your toolchain fresh.** Updates are on by default: take them as the channel delivers, do not pin.

### Stage self-check
Quality gate green and a coherent stage finished — PR opened or updated, or you are about to touch nodes beyond those you started from — re-read your branch diff against `main` for: bugs, brittle spots, weak error handling, DRY/SOLID violations, repeated patterns, missing or useless tests, files over 150 lines, god units. Fix in the **same branch** and push again — or say plainly nothing surfaced. Per stage, not only at the end.

### Cold stage review
Self-check does not replace cold review. After the self-check of an open/updated PR or a finished large stage, **open a review by the top-tier `reviewer` sub-agent** (`.claude/agents/reviewer.md`). Where it cannot run, say plainly that no cold review happened; do not pass off the self-check as one. **Only a push has a watchman**: a stage closed without a push reminds no one — review there is held by you alone, an acknowledged gap. The reviewer's field: the branch diff against `main`, the repository itself, the focus holon and its steward role, and references to the graph nodes the diff embodies — never a retelling of the session's reasoning. It returns findings plus an integration report; `NEEDS_CONTEXT` means a graph gap, not a briefing nit.

### Branch discipline
One branch until its merge — follow-ups go into it. After merge: `git checkout main && git pull`, delete the merged branch, weave the shipped state into the holon, confirm cleanup before the next task.

## Working principles
1. **Think before code.** Name assumptions; ask when unsure — naming *what* is unclear, in text (never the interactive option menu). Check repo + graph before writing; retrieve, do not recall. Touch the live system before trusting a type, a name, a doc. Questions beyond the boundary become `posed_to` vimarshas to the owner role — not silent decisions.
2. **Simplicity first.** Minimum code for the task. No speculative features, no abstractions for one-off code, no handling of impossible errors. Validate at boundaries; trust internal invariants.
3. **Stay inside the repo boundary.** A change belonging to another holon (vodopad, lightmare, another repo) is not yours across the boundary: record it as a vimarsha on that holon's node in its graph.
4. **A second implementation is a reportable event.** About to write what already exists? Derive both places via `iskron:integrity`, name them to the user, propose reunification or a named deliberate fork.
5. **Surgical changes.** Touch only what the task needs. No reformatting or refactoring of neighbouring code; the linter is authoritative. Delete only dead code your change produced; flag the rest.
6. **Goal-driven execution.** Tasks → verifiable goals. Bugs: pin with a failing test before the patch. Multi-step: plan as `step → check` pairs. Name the falsifier before looking and observe the carrier itself (*Reality*), not the source that should have produced it.
7. **Read before answering an open question.** Discuss / think through / investigate / design / plan / analyse — answered from recorded thinking, not training data: query the graph first, several ways. Driven by `iskron:entry`.
8. **Think in the graph, speak the project's language.** Structural vocabulary (kriya, phenomenon, holon, role, vimarsha, three mode axes) is for reasoning; it does not appear in what you say to the user unless they used it first. Talk *about* work in plain description — question, change, what is open, what it resolves — not ticket/sprint/backlog/story/done.

## Integration field — from the graph only
The focus holon and steward role above are the only permanent traversal root. A list of shared surfaces, consumers and dependencies is **not kept in AGENTS.md and not asked of the human**. For each change name the graph nodes the diff embodies and run `iskron:integrity`; trace phenomena both ways via `iskron_orient(lens="trace")`; a kriya's `next` thread and its `ahara`/`utpatti`/`upadhi` relays; an exit into another holon leads to its steward role. A dependency the traversal does not find is a model defect: design the missing kriyas/phenomena, weave edges via `iskron:weaving`, raise a vimarsha and wake the addressee via `iskron:collaborate`.

## External surfaces — what you use and do not own
This provider exists to call one: the **vodopad public API** (`api.fluence.dev`, OpenAPI 3.1). The agent *guesses* such surfaces by construction — memory is indistinguishable from knowledge from the inside.
- **Before work, pin the part of the surface the work will touch** — as a graph node, with the version you looked at. Current vendored snapshot: `internal/client/mock/testdata/fluence-public.yaml` (its `info.version` is the pin).
- **Source seniority — pratyakshita before shabda.** A live call or the mock's contract validator beats the spec; the spec beats memory; **memory is not a source**. Epistemic mode honestly: `pratyakshita` only for what you observed, `anumita` for what the spec says.
- **The spec can lag the code.** `../vodopad/docs/fluence-public.yaml` is regenerated by `cargo run --bin openapi` in the vodopad checkout (binary name `openapi`, not `gen_openapi`); the checked-in file lags source. When a route exists in `bin/vodopad/src/api/**` but not in the YAML, the source is the truth for wire shape and the YAML for what the contract middleware will accept — reconcile both before relying on either.
- **Weave the link.** The surface node is `upadhi` to the kriya that acts through it.
- **Keep in step.** On any drift or version bump, fix the node in the same move and lower its epistemic mode if not observed.
- **References work both ways.** Source touching the API carries `(graph @cloudless/fluence, node #N)` — and you **read that node before working**.

## Reality — what a claim is checked against
| Claim class | Canonical carrier | How to observe | Who can |
|---|---|---|---|
| wire format, resource lifecycle, plan/apply logic | the provider under test against the in-memory mock with contract enforcement | `make check` (unit tests + contract middleware; a path absent from the vendored spec is **not** checked — see gotchas) | agent |
| behaviour against the real API (statuses, polling, adopt-on-conflict, restart) | stage vodopad at `https://api.stage.fluence.dev` through the built provider | `make test-acc` with `.env` (`FLUENCE_API_KEY`, `FLUENCE_ENDPOINT`; loaded by direnv, git-ignored, template `.env.example`) | agent — stage key only; a production key never enters `.env` |
| docs and examples match the schema | regenerated `docs/` | `make docs-check` | agent |
| the release artifact installs and runs for a user | Terraform Registry / goreleaser output | after the first tag: `terraform init` against the published provider | user |

**Ceiling**: what vodopad does behind its API (lightmare provisioning, billing after provision, cluster capacity) — observable only through API status and the owner's word; the Registry install path — until the first tag exists. Neither closes as "verified".

**The table grows by use.** When a session teaches you a carrier this table does not hold, or a command here turns out wrong, write the row in that session before closing the work.

## Graph ↔ repo: where what lives
| Concern | Repo | Graph |
|---|---|---|
| Code, configs, lockfiles | ✓ | |
| Commands, conventions, gotchas, stack | ✓ (AGENTS.md) | |
| Branch state, what is in flight | git + PR body | ✓ (`genre=hint` — work without a PR) |
| Methodology, ontology | | ✓ |
| Design decisions, open questions | | ✓ (vimarshas) |
| Plans, task lists, session handovers | | ✓ (project graph) |
| Commit history, PRs, SHAs | git | (never in the graph) |

**`HANDOVER.md` is not created — a decision, not an omission.** Branch state already has homes and a hand-written file is the only one that drifts silently: branch and in-flight work — `git branch` / `log` and the open PR (`gh pr list`); how a claim is checked — *Reality* above; why it was decided and what is open — the graph; work in progress without a PR — a `genre=hint` seed. Branch state is stated **in the PR body**. If such a file exists, evacuate it into these homes and delete it.

## Commands
| Task | Command |
|---|---|
| build | `make build` (`go build ./...`) |
| vet | `make vet` |
| test (unit, mock + contract) | `make test` (`go test ./... -count=1`) |
| acceptance (live stage API) | `make test-acc` (reads `.env`; creates and destroys real stage resources — check `terraform`-side leftovers on failure) |
| **gate** | `make check` — gofmt, golangci-lint v2.7.1 (via `go run`, pinned in the Makefile), vet, build, unit tests, docs staleness. CI runs exactly this target. |
| lint only | `make lint` (config `.golangci.yml`) |
| format | `make fmt` |
| docs | `make docs` (tfplugindocs); `make docs-check` fails if `docs/` is stale |
| refresh vendored spec | `make openapi-refresh` (downloads the spec the stage API serves, normalizes to YAML; then fix whatever `make check` reports) |

Call the gate **by name**, never assemble the chain by hand. Delegation roles live in `.claude/agents/` (`reader`, `worker`, `verifier`, `reviewer`); AGENTS.md carries no delegation doctrine beyond this pointer.

## Project structure
- `main.go` — provider server entry.
- `internal/provider/` — resources, data sources, validators, `testing/` (mock harness) and `acctest/` (live-API harness).
- `internal/client/` — hand-written Go client for vodopad: `client.go` (`/v1`, `/v2`), `vm_draft.go` (`/v3` draft flow).
- `internal/client/mock/` — in-memory API mock; `contract_middleware.go` validates requests/responses against `testdata/fluence-public.yaml`.
- `docs/` — generated by tfplugindocs from `templates/` and `examples/`; never edited by hand. `docs/superpowers/` is legacy design prose — do not add to it.
- `examples/` — HCL examples that feed the docs.
- `.github/workflows/` — `build.yml` (PR gate), `edge.yml` and `release.yml` (goreleaser).

## Code conventions
- **Meaning lives in the graph, code references it.** A comment carrying a decision's rationale, rejected alternatives or integration layout is a graph node living away from home: move the meaning to the graph, leave `(graph @cloudless/fluence, node #N)` in code. Mechanics of a step — words in the comment; meaning, rationale, integration field — the graph. Readers of this code are agents with graph access; the repo is public on GitHub, so keep references terse and keep the code understandable without them.
- **English only** in everything committed (see "What this project is").
- **No co-author trailer, no "Generated with Claude" footer** on commits or PRs (`~/projects/CLAUDE.md`).
- **Wire format comes from the spec, not from memory.** Every new client path or field is added to the mock as well and passes the contract middleware; the contract test skips paths absent from the spec, so an unknown path passes silently — check the path exists in the YAML first.
- **Test discipline**: unit tests against the mock for every resource (`*_test.go`), contract tests in `internal/client`, acceptance tests (`*_acc_test.go`) gated by `TF_ACC=1`, never run in CI.
- **Gotchas**:
  - The vendored spec is fetched from the **running stage API** (`make openapi-refresh`, `OPENAPI_URL` in the Makefile), not from a vodopad checkout: the checked-in YAML in vodopad lags its source and vodopad may not compile locally. The contract middleware **skips** any path absent from the snapshot, so a green mock run only covers paths the snapshot names — refresh before touching a new endpoint.
  - Stage (0.11.1) is ahead of the public docs: security groups are created by `vpcId` (cluster derived), subnet create rejects `clusterId` (`additionalProperties: false`), moving a draft VM needs `expectedUpdatedAt` echoed verbatim. Assume every stage bump can break a request body; the gate catches it only after `make openapi-refresh`.
  - Catalog lists (`/v1/clusters`, `/v1/datacenters`, `/v1/configurations/virtual_machines`, `/v1/storages/default_images`) come wrapped as `{"items": […]}`; the mock contract test GETs them so an envelope change fails offline, not on stage.
  - Acceptance tests pick the first cluster the account sees (`data.cloudless_clusters.all.clusters[0]`), never a hard-coded region: the stage account has one cluster in LT.
  - Stage authenticates only by the `X-API-KEY` header; `Authorization: X-API-KEY …` alone is rejected. The client sends both, keep it that way.
  - The stage key for `make test-acc` must carry `vpc:write`, `subnet:write`, `security_groups:read`, `security_groups:write` besides the VM/storage/IP/ssh scopes; check with `GET /v1/api_keys` — a key without them fails every VPC-based test with 403 `UserVpcWrite`.
  - Destroyed resources stay readable on stage in status `removed`/`terminated` (soft delete); destroy checks treat those as gone (`acctest.GoneIf`).
  - `zsh` treats a leading `=` as command-path expansion; `echo ====` inside a compound command errors. Use `echo "## x"`.
  - `rtk` rewrites `grep`/`find` output into summaries; counts you act on must be computed with `awk` over the raw file.

## What to update when
- `AGENTS.md` — inverted default: **if it can be learned by reading a graph node, it is not here.** Update when commands, stack, conventions or reality carriers change.
- `docs/` — regenerate with `make docs` whenever a schema or example changes; CI checks staleness.
- `internal/client/mock/testdata/fluence-public.yaml` — refresh when vodopad's API moves; then fix whatever the contract test reports.
- The project graph — every merge (see "Session lifecycle").

## Git workflow
- **Conventional commits** (`feat:`/`fix:`/`chore:`/`refactor:`/`docs:`/`test:`/`ci:`). Branches `feat/…`, `fix/…`, `chore/…`. PR titles in the same format.
- **No co-author trailer**, no generated-with footer.
- **Forge**: GitHub (`fluencelabs/terraform-provider-cloudless`); CLI `gh`. Watch a PR with `gh pr checks --watch` on its branch.
- **Never take an outward action on the forge without an explicit ask in the current session** — no pushes, PRs or issues; commit locally on a branch by default.
- **Local gate — one call, not a list**: `make check` runs the whole chain; CI calls the same target on every PR to `main`. There is no pre-commit hook: run the gate before pushing.
- **Definition of done**: PR into `main`, `gh pr checks --watch` green, merged without conflicts.
- **Never** `--no-verify`, `--force`, `git reset --hard`, `rebase`, `amend`, `git add -A` without the user's explicit instruction.

*(iskronify: contract `6`, stamp `2026-09-06` — rerun when the installed iskronify's description names a higher contract, or when the sources this file derives from moved after this date. Owner interview `#1791` answered on 2026-09-06; stage API key for live runs agreed: `.env`, stage only.)*
