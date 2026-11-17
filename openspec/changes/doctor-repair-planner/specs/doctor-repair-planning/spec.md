## Purpose

Defines how `doctor --repair` turns a diagnosis of a broken setup into a confirmed, ordered repair plan and runs it step by step until the setup is clean or the user is told what still blocks it.

## ADDED Requirements

### Requirement: Repair recovers pending journals before configuration-dependent work

Repair SHALL plan pending dependency and ownership journals as safe stage-1 steps using the existing recovery operations without changing their recovery semantics. These steps SHALL require only journal authority, not valid configuration or resolved selection. Ownership recovery outcomes SHALL be printed. A refused recovery SHALL fail the step and exit 1 without changing the refused journal or target bytes. Off a terminal without `--yes`, repair SHALL preview pending journals without recovering them.

#### Scenario: Dependency journal recovery runs with invalid configuration

- **WHEN** a dependency journal is pending and configuration has errors
- **THEN** the first repair step SHALL be safe dependency replay and SHALL run when authorized
- **AND** a subsequent `sync --check` SHALL no longer report `dependency.recovery-required`, even if configuration errors remain

#### Scenario: Ownership journal recovery reports its outcome

- **WHEN** an ownership journal is pending and its safe recovery is authorized
- **THEN** repair SHALL recover it as a stage-1 step and print the recovery outcome

#### Scenario: Refused journal recovery preserves evidence and target bytes

- **WHEN** dependency recovery encounters foreign bytes or a non-prefix publication state
- **THEN** the repair step SHALL fail and doctor SHALL exit 1
- **AND** the journal and target bytes SHALL remain unchanged

#### Scenario: Pending journals are previewed off-terminal without consent

- **WHEN** pending dependency or ownership journals exist and repair runs off a terminal without `--yes`
- **THEN** repair SHALL print their safe recovery steps without replay
- **AND** journal and target bytes SHALL remain unchanged

### Requirement: Repair presents a numbered plan and one confirmation

`doctor --repair` SHALL print a numbered plan before it changes anything. Every plan line SHALL show the step number, its safety class (`safe`, `destructive`, `guided`, or `human-only`), the action, the diagnostic code it addresses, the files it touches, and either the inputs it needs or `blocked by N (<reason>)` naming the plan item that blocks it. MCP values SHALL never be printed. On a terminal, one `[y/N]` confirmation SHALL authorize every safe step; only an explicit `y` or `yes` is consent. Destructive entries SHALL keep their own per-entry confirmations, and a plan whose only runnable entries are destructive SHALL ask only those per-entry confirmations.

#### Scenario: Repair plan lists every step before confirmation

- **WHEN** `doctor --repair` runs on a terminal with at least one safe step to run
- **THEN** it SHALL print every plan step with its number, class, action, code, files, and inputs or blocker
- **AND** it SHALL ask exactly one plan-level `[y/N]` confirmation before running any step

#### Scenario: Declining the repair plan changes nothing

- **WHEN** the answer to the plan-level confirmation is anything other than `y` or `yes`, including an empty line or end of input
- **THEN** repair SHALL run no step and leave every configuration, lock, ownership-state, and harness file byte-identical
- **AND** repair SHALL exit 0 unless missing or stale acquisition authority remains, in which case it SHALL exit 1

### Requirement: Repair diagnoses again after every step

After each step runs, repair SHALL diagnose the setup again and recompute the plan. Steps that appear for the first time SHALL be shown and confirmed before they run; steps already confirmed or declined in this run SHALL NOT be offered again. A step that is still planned after it has run five times SHALL stop the repair.

#### Scenario: Steps added by re-diagnosis are confirmed before they run

- **WHEN** a step's result makes a new step necessary
- **THEN** repair SHALL show the new step and ask for confirmation before running it
- **AND** a declined new step SHALL NOT run

#### Scenario: A recurring step stops repair at the iteration cap

- **WHEN** a step remains in the plan after it has run five times
- **THEN** repair SHALL stop, name that step, and exit 1

### Requirement: Non-interactive repair runs only safe steps

Off a terminal without `--yes`, repair SHALL only preview the plan. With `--yes`, repair SHALL run safe steps without prompting. Destructive steps SHALL never run under `--yes` or off a terminal; they SHALL be reported and left unchanged.

#### Scenario: Off-terminal repair without --yes is preview-only

- **WHEN** `doctor --repair` runs off a terminal without `--yes`
- **THEN** it SHALL print the plan and write no file

#### Scenario: Repair with --yes runs only safe steps

- **WHEN** `doctor --repair --yes` finds safe and destructive steps
- **THEN** it SHALL run the safe steps
- **AND** every entry a destructive step would change SHALL remain byte-identical

### Requirement: Repair degrades per step instead of refusing

Invalid configuration and failed resolution SHALL NOT refuse the whole repair. A step SHALL run when every input it declares is valid. A step with an invalid input SHALL be listed as blocked by the plan item that reports the cause, and SHALL NOT run. A cause with no automated fix SHALL appear in the plan as a `human-only` item with its existing remedy text.

#### Scenario: Invalid configuration blocks only the steps that need it

- **WHEN** a configuration layer is invalid and the plan holds a step whose inputs do not include that layer
- **THEN** that step SHALL remain runnable
- **AND** every step that needs the invalid layer SHALL be listed `blocked by N (<reason>)` and SHALL NOT run

#### Scenario: Failed resolution blocks selection-dependent steps

- **WHEN** the selected resources cannot be resolved
- **THEN** repair SHALL list the resolution failure as a plan item and every step that needs the selection as blocked by it
- **AND** shared aggregate entries and acquisition locks SHALL remain unchanged

### Requirement: Repair revalidates policy before every step

Repair SHALL validate the configuration and acquisition-lock inputs that authorized its plan before each confirmed step and at each publication boundary. A changed input SHALL stop the repair with a generation conflict, run no further step, and preserve aggregate and ownership-state bytes.

#### Scenario: Policy change before a confirmed step stops repair

- **WHEN** configuration changes after the plan was confirmed and before a step runs
- **THEN** repair SHALL report a generation conflict, exit 1, and run no further step
- **AND** aggregate and ownership-state bytes SHALL be unchanged

### Requirement: Repair exit status reports whether the user is still stuck

Repair SHALL exit 0 only when the setup ends clean or the user declined without leaving missing or stale acquisition authority. It SHALL exit 1 when a step failed, corrupt state remains, missing or stale acquisition authority remains after preview or decline, or only blocked or human-only items remain.

#### Scenario: Repair exits zero when the setup ends clean

- **WHEN** every planned step ran and the final diagnosis is clean
- **THEN** repair SHALL exit 0

#### Scenario: Repair exits one when only blocked or human-only items remain

- **WHEN** the plan holds only blocked or human-only items
- **THEN** repair SHALL run no step and exit 1

#### Scenario: Repair exits one when a step fails

- **WHEN** a confirmed step fails
- **THEN** repair SHALL report the failure and exit 1

### Requirement: Repair refreshes writable acquisition locks without upgrading pins

Repair SHALL plan `lock.stale`, `lock.missing`, `lock.version-upgrade-required`, `lock.version-deprecated`, `lock.unsupported-version`, and `lock.change-required` in writable scopes as safe acquisition-lock steps using the lock reconciliation path without pin upgrades. Existing current pins SHALL remain byte-identical. Selection-dependent stages SHALL stay blocked until verified acquisition authority is available. Organization acquisition locks SHALL never be written.

#### Scenario: Confirmed lock repair retains current pins

- **WHEN** a writable acquisition lock has one of these diagnostics and its safe repair step is confirmed
- **THEN** repair SHALL reconcile it without upgrading any existing current pin
- **AND** a subsequent `sync --check` SHALL report no acquisition-lock diagnostic

#### Scenario: Organization lock repair remains human-only

- **WHEN** an organization acquisition lock needs repair
- **THEN** repair SHALL report a human-only item and leave that lock byte-identical

### Requirement: Repair fetches exact snapshots without changing locks

Repair SHALL plan `lock.offline-snapshot-missing` and `lock.snapshot-unavailable` as safe cache steps using the locked sync fetch path. Fetching SHALL preserve acquisition-lock bytes and verify the exact snapshot before any selection-dependent stage runs.

#### Scenario: Missing exact snapshot is fetched from its locked commit

- **WHEN** repair confirms a fetch for an unavailable exact snapshot
- **THEN** it SHALL fetch and verify the locked commit without changing acquisition-lock bytes

#### Scenario: Snapshot fetch failure stops repair

- **WHEN** the confirmed snapshot fetch fails because the registry cannot be reached
- **THEN** repair SHALL report the failed step, exit 1, and preserve acquisition-lock bytes

### Requirement: Lock regeneration requires destructive confirmation

Repair SHALL classify `lock.conflicted`, `lock.alias-forbidden`, `lock.merge-forbidden`, and `lock.duplicate-field` as destructive lock regeneration. Each lock replacement SHALL require a terminal, a fresh backup of the original bytes, and per-entry confirmation. It SHALL never run under `--yes`. Repair SHALL never delete a lock to bypass verification.

#### Scenario: Destructive lock regeneration preserves backup and agreed pins

- **WHEN** a user confirms regeneration of one of these writable locks on a terminal without `--yes`
- **THEN** repair SHALL create a fresh backup before replacing the lock
- **AND** it SHALL retain pins agreed by both conflict sides and one-sided pins only for resources still selected by configuration

#### Scenario: Divergent conflict pins require an explicit choice

- **WHEN** both conflict sides pin the same resource to different commits
- **THEN** repair SHALL ask which pin to keep before replacement
- **AND** off a terminal or under `--yes` the step SHALL remain guided and the lock SHALL remain byte-identical

#### Scenario: Destructive lock repair cannot run through safe automation

- **WHEN** destructive lock repair is previewed off a terminal, declined, or requested under `--yes`
- **THEN** repair SHALL leave the acquisition lock byte-identical

### Requirement: Lock verification failures remain human-only

Repair SHALL leave `lock.invalid`, `lock.manifest-mismatch`, `lock.digest-invalid`, and `lock.trust` human-only. Nested specific lock diagnostics SHALL determine classification before a generic invalid-lock wrapper. No automated repair SHALL bypass these verification failures.

#### Scenario: Verification refusal preserves acquisition-lock bytes

- **WHEN** an acquisition lock has one of these verification failures without a specifically supported repair diagnostic
- **THEN** repair SHALL report the human-only item and leave the lock byte-identical

### Requirement: Repair reconciles harness drift without changing acquisition locks

Repair SHALL plan harness drift that sync can reconcile as a safe step, after configuration and acquisition-lock steps and before shared-aggregate ownership repair. The step SHALL reconcile drift through sync from the existing acquisition locks and SHALL NOT change, create, or refresh any acquisition lock. An unmanaged shared-aggregate collision SHALL be left to shared-aggregate ownership repair.

#### Scenario: Drift repair leaves acquisition locks byte-identical

- **WHEN** a materialized harness file has drifted from the selected resources and repair runs the drift step
- **THEN** a subsequent sync check SHALL report no drift
- **AND** every acquisition lock SHALL be byte-identical to its state before repair

### Requirement: Shared-aggregate ownership repair is the final stage

Devin and Codex shared-aggregate ownership repair SHALL run as the last plan stage with its adoption, replacement, and orphan-removal classes, backups, and policy checks unchanged. Adoption is safe and is authorized by the plan confirmation rather than per-entry prompts. Replacement and orphan removal are destructive and SHALL retain their per-entry confirmations.

#### Scenario: Shared-aggregate repair runs after drift repair

- **WHEN** a plan holds both a drift step and shared-aggregate ownership candidates
- **THEN** the drift step SHALL be numbered and run before the shared-aggregate candidates

### Requirement: Repair offers non-interactive JSON plan output

`doctor --repair --json` SHALL write exactly one JSON document to stdout, containing plan steps with `id`, `code`, `class`, `action`, `files`, `status`, and `blocked_by`, and step results. It SHALL write no prose to stdout and SHALL never prompt. Without `--yes`, it SHALL write no repair files. With `--yes`, only safe steps SHALL run; destructive and guided steps SHALL report `deferred`. MCP environment and header values SHALL never appear in the output. Exit status SHALL match non-JSON repair for the same state. `--json` without `--repair` SHALL fail.

#### Scenario: JSON repair reports a structured plan and results

- **WHEN** `doctor --repair --json` diagnoses repair steps
- **THEN** stdout SHALL contain exactly one JSON document with the required plan-step fields and step results, and no prose

#### Scenario: JSON repair without yes never writes or prompts

- **WHEN** `doctor --repair --json` runs without `--yes`, including on a terminal
- **THEN** it SHALL never prompt and SHALL leave repair files unchanged

#### Scenario: JSON repair with yes runs only safe steps

- **WHEN** `doctor --repair --json --yes` finds safe, destructive, or guided steps
- **THEN** it SHALL never prompt, SHALL run only safe steps, and SHALL report destructive and guided steps as `deferred`

#### Scenario: JSON repair never prints MCP secrets

- **WHEN** MCP environment or header values contain a secret sentinel
- **THEN** JSON repair output SHALL not contain that sentinel

#### Scenario: JSON repair preserves text repair exit status

- **WHEN** JSON repair and non-JSON repair encounter the same state and authorization
- **THEN** they SHALL return the same exit status

#### Scenario: JSON without repair is rejected

- **WHEN** `doctor --json` runs without `--repair`
- **THEN** it SHALL exit nonzero with an error

### Requirement: Configuration errors offer guided fixes

Configuration field, YAML syntax, and duplicate-key errors in writable consumer layers SHALL appear as guided steps naming the authored file and line and showing a terminal-safe source snippet. Control characters SHALL be escaped. On a terminal without `--yes`, repair SHALL offer `$VISUAL`, or `$EDITOR` when `$VISUAL` is unset, and diagnose again after the editor exits. With neither editor configured, repair SHALL print manual instructions without treating the missing editor as an execution failure. Repair SHALL NOT rewrite configuration automatically.

#### Scenario: Configuration errors show safe source context

- **WHEN** a writable configuration has a field, YAML syntax, or duplicate-key error
- **THEN** repair SHALL show a guided step with the authored file and line and a source snippet whose control characters are escaped

#### Scenario: An editor fix is diagnosed again

- **WHEN** the user accepts a terminal guided step and the configured editor exits
- **THEN** repair SHALL diagnose again
- **AND** a still-broken file SHALL remain a guided step

#### Scenario: Missing editor leaves manual instructions

- **WHEN** a terminal guided step has neither `$VISUAL` nor `$EDITOR` configured
- **THEN** repair SHALL print manual editing instructions without an editor execution failure

#### Scenario: Non-interactive guided steps are deferred

- **WHEN** repair runs off a terminal or with `--yes`
- **THEN** guided steps SHALL be listed as deferred and no editor SHALL be launched

#### Scenario: Version and organization issues remain human-only

- **WHEN** diagnosis reports `v4.api-version-required`, `v4.api-version`, or an `organization.*` issue
- **THEN** repair SHALL list it as human-only with the existing remedy text
- **AND** its files SHALL remain byte-identical

#### Scenario: A guided layer does not block independent steps

- **WHEN** one layer has a guided error and another step uses only valid inputs from other layers
- **THEN** that step SHALL remain runnable

### Requirement: Repair inspects every declared native aggregate target

Shared-aggregate repair SHALL inspect every registered harness target whose native layout declares aggregates. Claude, Factory, Cursor, Windsurf, Gemini, and OpenCode SHALL use the existing exact-adoption, confirmed-replacement, and evidence-backed orphan-removal safety rules. Devin and Codex repair behavior SHALL remain unchanged. Conflict output SHALL describe values by digest only and SHALL NOT print MCP values.

#### Scenario: Repair includes every aggregate-capable target

- **WHEN** repair previews are requested
- **THEN** every registered target whose native layout declares aggregates SHALL be inspected

#### Scenario: Native aggregate repair retains action safety

- **WHEN** Claude, Factory, Cursor, Windsurf, Gemini, or OpenCode has an unmanaged exact desired entry, a differing desired entry, or an evidence-backed orphan
- **THEN** repair SHALL offer byte-neutral adoption only for the exact match, replacement only for the differing value, and removal only for the evidence-backed orphan
- **AND** replacement and removal SHALL require terminal per-entry confirmation and a fresh backup
- **AND** conflict output SHALL contain digests rather than MCP values

#### Scenario: Repair combines shared Gemini scopes

- **WHEN** the repository root is HOME and both scopes select Gemini aggregate entries
- **THEN** repair SHALL compute candidates once against the combined desired entries
- **AND** differing desired values for one identity SHALL report `materialize.destination.overlap`, offer no mutation of the shared file, and preserve settings and ownership state

#### Scenario: Claude repair refuses overlapping scope destinations

- **WHEN** `CLAUDE_CONFIG_DIR` places the machine Claude destination at or overlapping the repository Claude destination and both scopes select Claude aggregate entries
- **THEN** repair SHALL report `materialize.destination.overlap` and offer no adoption, replacement, or removal for either Claude scope
- **AND** settings and ownership state SHALL be preserved
