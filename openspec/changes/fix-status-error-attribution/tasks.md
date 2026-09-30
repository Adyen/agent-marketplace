## 1. Regressions first

- [x] 1.1 Add status tests for cross-scope and cross-kind attribution; confirm both fail against the original behavior.
- [x] 1.2 Add a failing materialization report test for the kind, installed name, harness target, and scope of a blocked resource.

## 2. Implementation

- [x] 2.1 Carry blocked resource identities in the materialization report and attribute status errors precisely.
- [x] 2.2 Bind both status scenarios with test annotations and preserve existing severity behavior.
- [x] 2.3 Review sync and TUI error attribution and run focused tests, validation, and spec checks.

## 3. Delivery

- [x] 3.1 Review and commit implementation locally without pushing.
- [ ] 3.2 `openspec archive fix-status-error-attribution --yes`
