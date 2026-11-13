## Purpose

Defines how `doctor --repair` turns a diagnosis of a broken setup into a confirmed, ordered repair plan and runs it step by step until the setup is clean or the user is told what still blocks it.

## ADDED Requirements

### Requirement: Repair presents a numbered plan and one confirmation

`doctor --repair` SHALL print a numbered plan before it changes anything. Every plan line SHALL show the step number, its safety class (`safe`, `destructive`, `guided`, or `human-only`), the action, the diagnostic code it addresses, the files it touches, and either the inputs it needs or `blocked by N (<reason>)` naming the plan item that blocks it. MCP values SHALL never be printed. On a terminal, one `[y/N]` confirmation SHALL authorize every safe step; only an explicit `y` or `yes` is consent. Destructive entries SHALL keep their own per-entry confirmations, and a plan whose only runnable entries are destructive SHALL ask only those per-entry confirmations.

#### Scenario: Repair plan lists every step before confirmation

- **WHEN** `doctor --repair` runs on a terminal with at least one safe step to run
- **THEN** it SHALL print every plan step with its number, class, action, code, files, and inputs or blocker
- **AND** it SHALL ask exactly one plan-level `[y/N]` confirmation before running any step

#### Scenario: Declining the repair plan changes nothing

- **WHEN** the answer to the plan-level confirmation is anything other than `y` or `yes`, including an empty line or end of input
- **THEN** repair SHALL run no step, exit 0, and leave every configuration, lock, ownership-state, and harness file byte-identical

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

Repair SHALL exit 0 only when the setup ends clean or the user declined. It SHALL exit 1 when a step failed, corrupt state remains, or only blocked or human-only items remain.

#### Scenario: Repair exits zero when the setup ends clean

- **WHEN** every planned step ran and the final diagnosis is clean
- **THEN** repair SHALL exit 0

#### Scenario: Repair exits one when only blocked or human-only items remain

- **WHEN** the plan holds only blocked or human-only items
- **THEN** repair SHALL run no step and exit 1

#### Scenario: Repair exits one when a step fails

- **WHEN** a confirmed step fails
- **THEN** repair SHALL report the failure and exit 1

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
