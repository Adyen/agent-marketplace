## 1. Implementation

- [x] 1.1 Update `toggleModalActionOptions` in `internal/tui/forms.go` and `renderToggleModal` / `runToggleModalAction` in `internal/tui/modal.go` so `[g]` toggles `scopeUserExclude` on rows with an overridable organization pin and `[l]` toggles `scopeLocalExclude` only when the row supports a personal-local override.
- [x] 1.2 Align bulk-toggle scope mapping in `internal/tui/bulk.go` and help bindings in `internal/tui/keys.go` with the unified `[g]`/`[l]` behavior while retaining the bulk `e` scope.
- [x] 1.3 Update `site/docs/reference/tui.md` to document the unified `[g]` and `[l]` toggles and their eligibility boundaries.

## 2. Verification

- [x] 2.1 Update and add spec-annotated Go tests in `internal/tui/modal_test.go`, `internal/tui/terminal_size_contract_test.go`, and `internal/tui/bulk_toggle_test.go`.
- [x] 2.2 Add or extend real-terminal TUI QA journeys under `tools/tui-qa/tests/` proving `[g]` org-default disable/restore and `[l]` local project override, and update `tools/tui-qa/README.md`.
- [x] 2.3 Run `make validate`, `make spec-check`, and `make tui-qa`.
