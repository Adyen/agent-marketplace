## 1. Specification

- [x] 1.1 Add the interactive-session inventory requirement to the `telemetry` delta
- [x] 1.2 Bind each scenario with a `// spec: telemetry/<scenario-slug>` test line

## 2. Pipeline suppression

- [x] 2.1 Add `WithoutInventory` and suppress `recordResolvedInventory` for marked contexts
- [x] 2.2 Test that marked contexts record nothing through `Resolve` and `RecordResolvedInventory`

## 3. TUI session recording

- [x] 3.1 Add the pending inventory holder and record only on flush
- [x] 3.2 Suppress inventory on the session context and flush before the session span ends
- [x] 3.3 Update telemetry tests for exit-only recording, failed final snapshots, and the Run exit path

## 4. Verification

- [x] 4.1 Run `make spec-check`
- [x] 4.2 Run `make validate`
- [x] 4.3 Run `openspec archive record-tui-inventory-at-exit --yes` in the same pull request as the implementation
