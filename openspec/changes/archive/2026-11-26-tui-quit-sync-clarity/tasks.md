## 1. Implementation

- [x] 1.1 Add a helper on `materialize.DriftReport` (e.g. `SyncableCount()`) separating `Missing + Orphaned` from `Broken` items without changing `InSync()` or `PendingCount()` semantics where full drift is reported.
- [x] 1.2 Update `dispatchAction(actionQuit)` in `internal/tui/app.go` and `renderConfirm` in `internal/tui/help.go` to prompt only when `SyncableCount() > 0` and to render a bounded, sanitized list of concrete `+`/`-` changes inside the modal frame.
- [x] 1.3 Update `syncDoneMsg` and `finishSyncReload` in `internal/tui/app.go` so `quitAfterSync` checks the authoritative post-sync reload when sync returned no hard error, keeping the TUI open with diagnostics if syncable drift remains while preserving `syncQuitErr` propagation on hard sync errors.

## 2. Verification

- [x] 2.1 Add spec-annotated Go tests in `internal/tui/confirm_sync_test.go` and `internal/materialize/`.
- [x] 2.2 Add or extend a real-terminal TUI QA journey under `tools/tui-qa/tests/` proving the itemized quit-sync prompt, broken-only quit, and unresolved post-sync retention, and update `tools/tui-qa/README.md`.
- [x] 2.3 Run `make validate`, `make spec-check`, and `make tui-qa`.
