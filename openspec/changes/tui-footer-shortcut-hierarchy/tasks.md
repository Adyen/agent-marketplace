## 1. Implementation

- [ ] 1.1 Update `hintItem` in `internal/tui/footer.go` and `shortcutKeyStyle` in `internal/tui/theme.go` so active action keys and floor keys share consistent emphasized key styling while descriptions stay muted.
- [ ] 1.2 Preserve colorless rendering and narrow-width admission priority (`footerFloor` ranks unchanged).

## 2. Verification

- [ ] 2.1 Add spec-annotated Go tests in `internal/tui/footer_hints_test.go` proving uniform key emphasis across color and colorless profiles and narrow-width floor retention.
- [ ] 2.2 Add or extend a real-terminal TUI QA journey under `tools/tui-qa/tests/` and update `tools/tui-qa/README.md`.
- [ ] 2.3 Run `make validate`, `make spec-check`, and `make tui-qa`.
