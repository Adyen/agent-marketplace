# Deterministic TUI QA Journeys

**Date:** 2026-09-04
**Status:** Approved

## Problem

The direct PTY suite added for the TUI is nondeterministic in CI. The local-toggle-and-sync journey has failed at multiple unrelated points:

- the Available section became collapsed before the resource was selected;
- selection was lost before the toggle modal opened;
- the local config was written, but `s` was handled while the model still reported the old clean drift state, so no sync ran.

The current test sends several state-changing keys without waiting for their effects. It also accepts `/Sync complete|in sync/`, allowing the pre-existing `~ in sync` footer to satisfy the post-sync assertion. File creation proves that a command wrote config, but not that the TUI consumed the completion and reloaded state. The single-skill fixture also makes search coverage vacuous.

## Scope

Replace the current five mixed journeys with eight short interaction contracts. Keep `@microsoft/tui-test` pinned at `0.0.4`, use one worker, and retain the existing ownership boundary:

- Go tests own model state, asynchronous behavior, rendering contracts, key aliases, and terminal-guard internals.
- Go E2E owns configuration, resource, scope, collision, ownership, harness, and materialization matrices.
- `tools/tui-qa` owns canonical user inputs through a real PTY and their visible outcomes.

This work is QA-only. Do not change production TUI behavior, add test-only product markers, add dependencies, create external scenario schemas, or change release dependencies or repository settings.

## Deterministic step contract

Add one small TypeScript `step(name, action, assertion)` helper. It may only attach the step name to an ordinary assertion failure. Keep every key and expectation inline in the journey file; do not add a class, action registry, scenario runner, result format, retry layer, or domain helper that hides several inputs.

Every state-changing input must be followed by a causal observable barrier before the next input:

- begin only after both fixture resources are visible and startup refresh has settled;
- assert semantic user-visible text, not style or coordinates;
- use an outcome that could not have satisfied the step before its action;
- use positive and negative controls when needed for non-vacuity;
- use short bounds for UI/process transitions and longer bounds only for Git/materialization;
- use identical timeout behavior locally and in CI;
- never use fixed sleeps, terminal-idle inference, CI timeout multipliers, or retries.

Use 5 seconds for ordinary UI/process barriers and 30 seconds for Git/materialization barriers unless measurements show an existing operation-specific bound is insufficient.

Failures use the test runner's ordinary console output and current terminal screen only. Remove snapshots, raw trace configuration, artifact uploads, custom artifact environment variables/helpers, and the standalone privacy test.

## Fixture

Each PTY journey gets a fresh isolated HOME, XDG roots, workspace, temporary directory, and local bare Git registry. Fixture definitions must be side-effect-free during test discovery; filesystem and process setup/cleanup belongs in test lifecycle hooks so discovery-process imports cannot leak temporary sandboxes.

The shared registry contains exactly two skills:

- `beta-skill`, selected by project config and materialized by fixture setup;
- `alpha-skill`, available but unselected.

This baseline gives search a distractor and makes Scope, Registry, Status, pending drift, and post-sync states distinguishable without adding a policy matrix. Use one Factory target. The non-TTY journey uses a minimal isolated process sandbox and does not create a registry.

## Journeys

Put each journey in its own test file.

### 1. Launch, browse, inspect, and quit

1. Launch the binary with no subcommand in an 80x24 PTY.
2. Wait for `beta-skill`, `alpha-skill`, and settled `~ in sync`.
3. Press `2`; require registry-group content unique to Registry.
4. Press `3`; require both Synced and Available status groups.
5. Press `1`; require Project and Available scope groups.
6. Press `G`; require inline details for available `alpha-skill`.
7. Press Enter; require the full `alpha-skill` detail view.
8. Press Esc; require the list and the same selection.
9. Press `q`; require exit code 0.

### 2. Search and select

1. Launch explicit `agent-marketplace tui` and wait for the settled baseline.
2. Press `/`; require the anchored search-input line.
3. Type `alpha`; require `alpha-skill`, require one result, and require `beta-skill` absent.
4. Press Enter; require the search input absent while inline `alpha-skill` details remain selected.
5. Press `q`; require exit code 0.

### 3. Local toggle and sync

1. Launch explicit TUI and wait for the settled baseline.
2. Prove `alpha-skill` is not materialized.
3. Press `G`; require inline `alpha-skill` details.
4. Press Space; require the scope modal and `toggle local` action.
5. Press `l`; wait for `alpha-skill` to become local/pending and exactly `1 out of sync`.
6. Assert exact local config bytes and prove `alpha-skill` remains unmaterialized.
7. Press `s`; require unique `Sync complete`, then settled `~ in sync`.
8. Assert the generated Factory skill symlink and expected content.
9. Press `q`; require exit code 0.
10. Run public `sync --check`; require exit code 0.

### 4. Help and return

1. Launch explicit TUI and wait for the settled baseline.
2. Press `G`; require inline `alpha-skill` details.
3. Press `?`; require the Keyboard Shortcuts overlay and canonical actions.
4. Press `?`; require the overlay absent and the same selection restored.
5. Press `q`; require exit code 0.

### 5. Resize and recover

1. Launch at 80x24, wait for the settled baseline, and select `alpha-skill`.
2. Resize to 49x11.
3. Require `Resize required`, `Minimum terminal: 50x12`, and quit guidance; require normal resource content hidden.
4. Press Space; require the resize screen unchanged and no toggle modal.
5. Resize to exactly 50x12.
6. Require the prior `alpha-skill` selection and normal controls restored.
7. Press `q`; require exit code 0.

### 6. Ctrl+C hard exit

1. Launch explicit TUI at 50x12 and wait for the ready list and quit guidance.
2. Send Ctrl+C.
3. Require a clean bounded process exit.

### 7. SIGTERM hard exit

1. Launch explicit TUI and wait for the ready list.
2. Read the launcher-published target PID and send SIGTERM.
3. Require bounded PTY exit and prove the target PID no longer exists.
4. Do not require one numeric signal exit code across macOS and Linux.

### 8. Non-TTY entrypoints

In one minimal process test:

1. Bare non-TTY invocation exits 0 and prints Cobra help.
2. Explicit non-TTY `tui` exits nonzero promptly and reports that an interactive terminal is required.

## Runner and CI

- Keep Node 24, pnpm 11.20.0, frozen dependencies, `@microsoft/tui-test` 0.0.4, zero retries, and one worker.
- `make tui-qa-check` typechecks and verifies the PTY adapter.
- `make tui-qa` builds the e2e-tagged binary and runs all eight journeys once.
- Run the normal TUI QA job as a `ubuntu-latest` / `macos-latest` matrix on every pull request and every `main` update, with `fail-fast: false`.
- Retain an aggregate check named `TUI QA Driver` that succeeds only after both matrix legs succeed, preserving the existing required status context.
- Do not change branch protection, repository rules, or `release.needs`.
- Add a separate scheduled and manually dispatchable Linux workflow. Build/install once, then run the complete compiled journey suite 20 consecutive times, sequentially, with zero retries; stop on the first failure and use console output only.

## Implementation order

1. Rewrite local-toggle-and-sync as the tracer journey and remove its old flaky version.
2. Prove the tracer passes 20 consecutive times on Linux and 20 on macOS.
3. Retain only the single step helper and fixture behavior the tracer demonstrates.
4. Add the other seven journey files one at a time, keeping the suite green.
5. Remove the old mixed spec and all snapshot/trace/artifact/privacy machinery.
6. Add the cross-platform normal CI matrix and nightly Linux stress workflow.
7. Update the existing TUI QA README, `AGENTS.md`, and existing test-policy wording that still describes five journeys or emitted snapshots.

## Acceptance criteria

- All eight journeys pass once with one worker on Linux and macOS without retries.
- The local-toggle-and-sync tracer passes 20 consecutive times on each platform.
- The nightly Linux workflow runs the complete suite 20 times without rebuilding between iterations.
- No journey sends a second state-changing input before observing the first input's causal visible outcome.
- Search has a positive match and a disappearing distractor.
- Toggle visibly reaches pending drift before sync; sync uses a unique completion marker; final config, symlink/content, and `sync --check` all agree.
- Fixtures leave no discovery-process temporary sandboxes.
- CI produces no TUI QA snapshots, traces, or uploaded artifacts.
- `make tui-qa-check`, `make tui-qa`, `make test`, `E2E_SKIP_DOCKER=1 E2E_PARALLEL=4 make e2e`, `make ci-lint`, and `make build` pass.
