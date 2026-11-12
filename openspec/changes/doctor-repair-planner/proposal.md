## Why

`doctor --repair` only recovers Devin and Codex shared-aggregate ownership, and it refuses outright whenever configuration or resolution fails. That is exactly when users need it: an interrupted write, a stale or conflicted acquisition lock, a configuration error, or a collision in another harness's shared files leaves the user chaining commands from hints, and doctor's own fallback hint points back at doctor.

## What Changes

- `doctor --repair` diagnoses the setup, prints a numbered repair plan, asks one confirmation, runs one step at a time, and diagnoses again after every step until the setup is clean, only blocked or human-only items remain, a step fails, or a step keeps recurring.
- Every plan step has a safety class. One confirmation authorizes the safe steps; destructive entries keep their per-entry confirmations and never run under `--yes` or off a terminal. Off a terminal without `--yes`, repair only previews.
- Invalid configuration, failed resolution, and organization acquisition failure no longer refuse the whole repair. Steps whose inputs are valid still run; the others are listed as blocked by the plan item that explains the cause.
- Repair adds a harness drift stage that reconciles drift through sync without changing any acquisition lock.
- The existing Devin and Codex shared-aggregate repair becomes the last stage of the plan with its candidate classes, prompts, and policy checks unchanged.
- Exit status reflects whether the user is still stuck: exit 0 only when the setup ends clean or the user declined.
- Later work in this change adds journal recovery, acquisition lock, configuration, and further shared-aggregate fixers, and JSON plan output.

## Capabilities

### New Capabilities

- `doctor-repair-planning`: the diagnose, plan, confirm, execute, and re-diagnose loop of `doctor --repair`, its safety classes, degraded inputs, drift stage, and exit status.

### Modified Capabilities

- `owned-entry-reconciliation`: organization acquisition failure blocks every selection-dependent repair step instead of refusing the entire repair.

## Impact

`doctor --repair` output, prompts, and exit status; the `doctor` command help. No configuration, lock, ownership-state, or harness file format changes. Harness lists and organization files are never edited.

## Release note

`doctor --repair` now shows a numbered repair plan, asks once before running its safe steps, repairs harness drift, and keeps working when configuration or resolution is broken by listing the steps it cannot run yet and why.
