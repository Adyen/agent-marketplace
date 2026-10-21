## 1. Tests

- [x] 1.1 Add Go tests for drift precedence on a selected row (missing over orphaned, broken over both), annotated with `// spec: tui-interactions/retargeted-resource-is-pending-sync`, and for an orphaned copy alone showing as pending sync, annotated with `// spec: tui-interactions/removed-target-harness-is-pending-cleanup`

## 2. Implementation

- [x] 2.1 Share one drift-by-resource helper between `buildRows` and `applyDriftItems` with precedence broken, missing, orphaned, and show a row's orphaned copy as pending sync

## 3. Journey, documentation and archive

- [x] 3.1 Add the `harness-project-scope` TUI QA journey with its README entry, and verify `make spec-check`
- [x] 3.2 Run `openspec archive tui-retargeted-resource-pending --yes`
