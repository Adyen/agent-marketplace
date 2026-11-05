## 1. Regression proof

- [x] 1.1 Add `tui-error-recovery` annotated TUI tests for category defaults, errors and warnings, typed causes, lock guidance, interrupted writes, operation failure lifetime, and terminal safety; verify they fail on the base.
- [x] 1.2 Add an annotated sync reload regression for a failed verification with no specific cause; verify it fails on the base.

## 2. Implementation

- [x] 2.1 Add `internal/tui/error_recovery.go` with the recovery helper and sentinel; wire `renderErrors` and the verification fallback in `app.go`.
- [x] 2.2 Add writable-scope snapshot and manifest lock guidance with organization precedence, and a per-render category map.
- [x] 2.3 Verify the new tests and existing sync reload, diagnostics, error selection, and terminal rendering tests pass; record the render benchmark plumbing run.

## 3. Integration

- [x] 3.1 Run `make validate TEST_PACKAGES=./internal/tui`, `make spec-check`, and `make tui-qa` for the handoff gate; all pass.
