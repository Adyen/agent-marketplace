# Doctor Repair Planner

Approved scope: turn `doctor --repair` from a Devin/Codex shared-aggregate
ownership tool into the single command that takes a user from a broken setup
back to a working one. The normative contract will live in the
`doctor-repair-planner` OpenSpec change; this document records the design and
the work breakdown.

## Problem

Users have reported four kinds of broken setup, and `doctor --repair` fixes
none of them unless the cause happens to be aggregate ownership:

| Case | Example diagnostic | What happens today |
| --- | --- | --- |
| (a) Interrupted write | `dependency.recovery-required` | Hint says to run `sync`/`lock`; repair does nothing |
| (b) Lock problems | `lock.stale`, `lock.missing`, `lock.conflicted`, `lock.offline-snapshot-missing` | Resolve fails, so repair refuses before previewing |
| (c) Aggregate collisions | Shared hook and MCP entries in any target's aggregate files | Only Devin and Codex are covered |
| (d) Config errors | unknown field, malformed YAML | `len(ec.Errors) > 0` makes repair refuse outright |

Three structural causes:

1. Repair has one domain: `materialize.RepairPreviews` for Devin and Codex.
2. Repair refuses when configuration or resolution fails
   (`runDoctor` and `runDoctorRepair` in `cmd/agent-marketplace/doctor.go`),
   which is exactly when a user's setup is broken.
3. `internal/remedy` already maps codes to hints, and many hints are a
   mechanical command, but doctor never executes them. The `doctor` fallback
   hint ("rerun `doctor --debug`") is circular.

## Decisions

| Decision | Choice | Rejected alternatives |
| --- | --- | --- |
| Authority | Doctor executes every fix itself, reusing existing code paths for `lock`, `sync --locked`, journal recovery, and drift sync | Plan-and-hand-off (user still chains commands); risk-tiered split (two mental models) |
| Execution model | Diagnose, plan, confirm, execute one step, re-diagnose, repeat | One-shot plan from a single diagnosis (misses cascades such as a journal replay clearing `lock.stale`) |
| Confirmation | One plan-level confirmation authorizes every safe step; destructive entries keep per-entry prompts; a re-diagnosis that adds steps asks again for the new steps only | Per-step confirmation (tedious); plan plus per-network/lock-write step (two prompt rules) |
| Broken inputs | Degrade per step: a step runs when its declared inputs are valid; other steps are listed as blocked with the reason | Refuse the whole repair (today's behavior) |
| Delivery | One OpenSpec change, one feature branch `feat/doctor-repair-planner`, one bead per slice, archive as the final commit of the single PR to `main` | One change per bead (moving core spec under fixers); land core to `main` early (no long-lived branch, but more churn) |

Out of scope: setting aside a broken personal configuration layer (needs a
separate security decision), TUI integration, automatic v4 conversion (the
shipped CLI has no migrator), and any change to organization files.

## Architecture

```
 diagnose --> plan --> show plan --> confirm --> run step --> re-diagnose
                ^                                                  |
                |         plan changed? show new steps, confirm    |
                +--------------------------------------------------+
   stop when: clean | only blocked/human-only items remain | a step failed | iteration cap
```

### Package `internal/repair`

- `Diagnosis`: built from data doctor already loads, so `doctor` and
  `doctor --repair` cannot disagree about what is wrong:
  `pipeline.LoadEffective` errors, `ResolveErrors`, dependency and ownership
  journal state, and `materialize.RepairPreviews`.
- `Fixer`: keyed by diagnostic code. Declares its stage, the inputs it needs,
  its safety class, the files it touches, and `Apply(ctx) error`.
- `Planner`: orders steps by stage, marks steps whose inputs are invalid as
  blocked by the step that would fix them, and attaches human-only items.
- Loop: runs one confirmed step, rebuilds the diagnosis, and recomputes the
  plan. The iteration cap is 5; reaching it is a failure that names the step
  that kept recurring.

### Stage order

1. Journal recovery (needs only the journals; runs before config validation,
   as the spec already allows recovery-capable commands to do).
2. Configuration (guided).
3. Acquisition locks.
4. Cache and snapshots.
5. Harness drift.
6. Shared-aggregate ownership (today's repair, moved in unchanged).

### Safety classes

| Class | Examples | Off-terminal / `--yes` |
| --- | --- | --- |
| safe | journal replay, `lock` without pin upgrades, `sync --locked` fetch, drift `sync`, exact-match adoption, cleanup of attempt-owned staging | Runs under `--yes`; off-terminal without `--yes` is preview-only |
| destructive | conflict replacement, orphan removal, lock regeneration | Terminal only, fresh backup, per-entry confirmation, never under `--yes` |
| guided | configuration syntax or field errors | Shows `file:line` and a snippet, offers `$EDITOR` on a terminal, re-diagnoses |
| human-only | v4 version refusal, organization trust and locks, lock verification failures | Printed with the existing remedy text; never attempted |

### Invariants carried over

- Policy revalidation (`dependencytxn.ValidateReadAssertions`) runs before each
  confirmed step and at each publication boundary; a change stops the whole
  repair with a generation conflict, preserving "Repair publication uses
  unchanged policy" across the plan.
- No ownership lock is held while waiting for confirmation.
- Organization acquisition failure blocks every selection-dependent step,
  preserving "Repair refuses incomplete organization selection"; journal and
  configuration steps still run.
- Git-backed desired values come from valid acquisition locks and verified
  exact snapshots ("Repair uses verified acquisition authority").
- Doctor still never edits harness lists.

### Exit codes

- 0: the setup ends clean, or the user declined.
- 1: a step failed, corrupt state remains, or only blocked or human-only items
  remain. The user is still stuck, so success would be the wrong signal.

### Output

Every plan line shows the step number, class, action, the diagnostic code it
fixes, the files it touches, and either its inputs or what blocks it:

```
 plan
  1. [safe]       replay interrupted dependency write              (needs: journal only)
  2. [guided]     fix .agents/config.yaml:14 unknown field "harness"
  3. [safe]       lock --scope repo                                blocked by 2 (repo layer invalid)
  4. [safe]       adopt 3 Codex MCP entries                        blocked by 3 (selection unresolved)
  5. [human-only] organization lock missing: ask your administrator
```

MCP values are never printed (they may hold secrets); conflicts keep the
existing digest-only description.

## Fixers

| Bead | Diagnostics | Action | Class |
| --- | --- | --- | --- |
| B1 | `dependency.recovery-required`, pending ownership journals | `pipeline.RecoverDependencyTransactions`, `ownership.Store.Recover` | safe |
| B2 | `lock.stale`, `lock.missing`, `lock.version-upgrade-required`, `lock.version-deprecated`, `lock.unsupported-version`, `lock.change-required` | The `lock` code path without pin upgrades | safe |
| B2 | `lock.offline-snapshot-missing`, `lock.snapshot-unavailable` | The `sync --locked` fetch path | safe |
| B2 | `lock.conflicted`, `lock.alias-forbidden`, `lock.merge-forbidden`, `lock.duplicate-field` | Regenerate from configuration, keeping pins both sides agree on; back up the old lock | destructive |
| B2 | `lock.manifest-mismatch`, `lock.digest-invalid`, `lock.trust` | Never automated; existing "do not bypass verification" remedy | human-only |
| B3 | Configuration field, YAML, and duplicate-key errors | `file:line` snippet, `$EDITOR` offer, re-diagnose | guided |
| B3 | `v4.api-version-required`, `v4.api-version`, `organization.*` | Existing remedy text | human-only |
| B0 | Harness drift reported by sync check | `runSyncWithLocks` limited to drift, no pin changes | safe |
| B4 | Shared aggregates of every target that declares `NativeLayout.Aggregates` (Claude, Factory, Cursor, Windsurf, Gemini, OpenCode) | Drive `RepairPreviews` from the layout registry instead of a Devin/Codex list | existing adopt/replace/orphan classes |
| B5 | All | `doctor --repair --json` emits the plan and step results (code, class, files, status) | n/a |

### Conflicted lock regeneration

When both sides of a lock merge conflict pin the same resource to different
commits, doctor must not choose. On a terminal it asks which pin to keep; off
a terminal, or under `--yes`, the step stays guided and the lock is unchanged.
Pins that only one side carries are kept when configuration still selects the
resource.

### JSON mode

`--json` never prompts, so it behaves like an off-terminal run: preview-only
without `--yes`, safe steps only with `--yes`. Destructive and guided steps
appear in the output as deferred.

## Testing

- Every scenario in the spec delta is bound with a `// spec:` annotation.
- Command tests use the isolated `isolateCommandTest` pattern.
- Each bead's red test fails before its change.
- One end-to-end broken-to-healthy test per case, in `e2e/`, asserting that
  `sync --check` is clean afterwards:
  - (a) crash injected with `AGENT_MARKETPLACE_E2E_CRASH_AT`
  - (b) acquisition lock with Git conflict markers
  - (c) shared aggregate collision
  - (d) configuration with an unknown field, fixed through a scripted
    `$EDITOR`
- `TestPublicCommandPathsHaveBehaviorTests` gets a witness for `--json`.
- Existing `doctor_*_test.go` and `e2e/doctor_repair_policy_test.go` keep
  passing unchanged, proving stage 6 kept its contract.
- No TUI files change; commits carry `TUI-QA-Impact: none` only while that
  holds.

## Work breakdown

All beads land on `feat/doctor-repair-planner`. B0 blocks B1 to B5; B7 depends
on all of them.

1. **B0** Planner core: `internal/repair`, diagnosis, plan rendering,
   confirmation, re-diagnose loop, degrade-instead-of-refuse, exit codes,
   drift stage, aggregate repair moved in as stage 6, OpenSpec change
   scaffolding with the core spec delta.
2. **B1** Journal recovery fixer.
3. **B2** Lock fixers, including conflicted-lock regeneration.
4. **B3** Guided configuration fixes and human-only items.
5. **B4** Aggregate repair for every target with declared native aggregates.
6. **B5** `--json` plan output.
7. **B7** End-to-end journeys for cases (a) to (d), spec and docs
   reconciliation, `openspec archive`, and the PR to `main`.

## Open items

- The `openspec` CLI on the author's machine fails with a missing Node
  interpreter; it must work before B0 scaffolds the change.
- Gemini shares one settings file across scopes when the repository is HOME
  ("Gemini shared scopes reconcile together"); B4 must keep that combined
  reconciliation and its overlap refusals.
