## 1. OpenSpec and baseline

- [x] 1.1 Create `tui-review-input-and-reload` with navigation, interactions, and startup deltas; verify each new scenario has an intended test binding.
- [x] 1.2 Correct the archived Marketplace full-view design spacing typo and verify the line reads `at 50/80 columns`.
- [x] 1.3 Capture the exact requested TUI baseline benchmarks to `/tmp/pr456-review-artifacts/benchmarks-before.txt` before production edits.

## 2. Regression witnesses (test first)

- [x] 2.1 Isolate both real Sync phase-observer tests under a temporary HOME and nested XDG/CODEX roots; verify no environment state escapes.
- [x] 2.2 Cover startup-only provisional/config publication versus one final ordinary reload at the existing snapshot seam; assert discovery/load counts, pending-row publication, identical final row identity/drift, and no mutation.
- [x] 2.3 Exercise slash through actual toggle and config-editor modal handlers in a bounded child process; verify no selection/write change and later cancel/navigation remains available.
- [x] 2.4 Cover full-to-compact-to-full resizing in both harness dialogs; verify every choice, unknown selected ID, cursor, and selected IDs.
- [x] 2.5 Verify `j` and `k` append to a non-empty command-palette query and do not dispatch/navigation; keep arrow-key tests and require the full qualified fixture row under the palette layer.
- [x] 2.6 Deliver source A's captured progress poll after source B begins; verify B remains the active progress source and no command is scheduled.
- [x] 2.7 Use available and project-enabled rows in search coverage; verify both sections exist before filtering and only the enabled section remains for `deploy`.
- [x] 2.8 Capture the dark key style before constructing the second model and verify it is unchanged afterward.
- [x] 2.9 Strengthen PTY wheel-burst detail assertions and add a bounded real-parser Go witness for the exact encoded 37-event sequence and final entry04 state.

## 3. Minimal implementation

- [x] 3.1 Gate provisional, browse-config, and config callbacks on progressive startup while keeping the resolved callback and final assessment for ordinary reloads; verify snapshot regressions pass.
- [x] 3.2 Disable filtering on only the fixed toggle and config-editor Select fields; verify bounded slash regressions pass.
- [x] 3.3 Restore full/compact harness MultiSelect height through one shared helper without changing stored selections or cursor; verify both resize regressions pass.
- [x] 3.4 Route only literal up/down palette messages to cursor navigation; verify j/k query regressions pass.

## 4. Verification

- [x] 4.1 Run each changed package's focused red/green tests and retain verbose logs under `/tmp/pr456-review-artifacts/`.
- [x] 4.2 Run `CGO_ENABLED=0 make validate TEST_PACKAGES='./internal/tui ./internal/pipeline' TEST_FLAGS='-count=1'`; retain its log and exit status.
- [x] 4.3 Run `make spec-check`, `make tui-qa`, the affected CLI witness, and `CGO_ENABLED=0 go -C e2e test -run 'TestMarketplaceTUI' -count=1 .`; retain logs and exit statuses.
- [x] 4.4 Save the completed uncommitted diff, OpenSpec status, and raw post-change benchmark output for lead review.

## 5. Finalization

- [x] 5.1 Archive this OpenSpec change only after the final gates and commit organization are explicitly settled.
