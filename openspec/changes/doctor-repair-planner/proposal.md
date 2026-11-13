## Why

`doctor --repair` only recovers Devin and Codex shared-aggregate ownership, and it refuses outright whenever configuration or resolution fails. That is exactly when users need it: an interrupted write, a stale or conflicted acquisition lock, a configuration error, or a collision in another harness's shared files leaves the user chaining commands from hints, and doctor's own fallback hint points back at doctor.

## What Changes

- `doctor --repair` diagnoses the setup, prints a numbered repair plan, asks one confirmation, runs one step at a time, and diagnoses again after every step until the setup is clean, only blocked or human-only items remain, a step fails, or a step keeps recurring.
- Every plan step has a safety class. One confirmation authorizes the safe steps; destructive entries keep their per-entry confirmations and never run under `--yes` or off a terminal. Off a terminal without `--yes`, repair only previews.
- Invalid configuration, failed resolution, and organization acquisition failure no longer refuse the whole repair. Steps whose inputs are valid still run; the others are listed as blocked by the plan item that explains the cause.
- Repair adds a harness drift stage that reconciles drift through sync without changing any acquisition lock.
- The existing Devin and Codex shared-aggregate repair becomes the last stage of the plan with its candidate classes, destructive per-entry prompts, and policy checks unchanged; safe adoption is now approved by the plan confirmation.
- Exit status reflects whether the user is still stuck: exit 0 only when the setup ends clean or the user declined.
- Later work in this change adds journal recovery, acquisition lock, configuration, and further shared-aggregate fixers, and JSON plan output.

## Capabilities

### New Capabilities

- `doctor-repair-planning`: the diagnose, plan, confirm, execute, and re-diagnose loop of `doctor --repair`, its safety classes, degraded inputs, drift stage, and exit status.

### Modified Capabilities

- `owned-entry-reconciliation`: organization acquisition failure blocks every selection-dependent repair step instead of refusing the entire repair.

## Impact

`doctor --repair` output, prompts, and exit status; the `doctor` command help. No configuration, lock, ownership-state, or harness file format changes. Harness lists and organization files are never edited.

## Release and upgrade

This prompt change must ship in a maintainer-selected major CLI release, not a patch or minor release; maintainers select the exact version.

Before upgrading repair workflows, preview off a terminal with `agent-marketplace doctor --repair </dev/null` and review the new prompts. Remove old positional answer streams; use `--repair --yes` only when all safe steps are intended, and review destructive per-entry prompts separately on a live terminal. No configuration or lock migration is needed.

## Release note

Breaking: `doctor --repair` replaces per-entry adoption prompts with one plan confirmation for all safe steps, including harness drift repair. Stop replaying old terminal answers and review the new prompts; use `--repair --yes` for safe-only automation, which leaves destructive entries untouched.
