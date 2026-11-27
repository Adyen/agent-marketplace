## 1. Implementation

- [x] 1.1 Track optional organization selection and parent collection names cleanly on `session.Row` in `internal/session/rows.go` and `internal/session/catalog.go`.
- [x] 1.2 Update `originBadge` in `internal/tui/badges.go` to render `O` for optional organization defaults (distinct from `G` user-personal and `R` required organization).
- [x] 1.3 Render `Source layer:` and `Included via:` fields in `internal/tui/detail.go` with terminal sanitization.

## 2. Verification

- [x] 2.1 Add spec-annotated Go tests in `internal/tui/badges_test.go`, `internal/tui/detail_test.go`, and `internal/session/`.
- [x] 2.2 Add or extend a real-terminal TUI QA journey under `tools/tui-qa/tests/` proving `O` badge and detail provenance display, and update `tools/tui-qa/README.md`.
- [x] 2.3 Run `make validate`, `make spec-check`, and `make tui-qa`.
