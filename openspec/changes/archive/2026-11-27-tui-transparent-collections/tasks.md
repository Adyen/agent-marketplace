## 1. Implementation

- [x] 1.1 Add read-only verified collection manifest loading for Marketplace collections in `internal/session/` using the exact manifest digest and endpoint verification.
- [x] 1.2 Render collection members and included sub-collections in `internal/tui/detail.go` and `internal/tui/marketplace_detail.go`.
- [x] 1.3 Extend `internal/tui/tree.go` and `internal/tui/list.go` so collection rows expand/collapse (`▼`/`▶`) across Status, Source, and Kind groupings like plugin parent rows.
- [x] 1.4 Route toggling an available member row beneath an expanded collection through the direct single-resource install/toggle pipeline in `internal/tui/modal.go` and `internal/tui/discovery.go`.

## 2. Verification

- [x] 2.1 Add spec-annotated Go tests in `internal/tui/tree_test.go`, `internal/tui/detail_test.go`, `internal/tui/marketplace_inspection_test.go`, and `internal/tui/installed_collections_test.go`.
- [x] 2.2 Add a real-terminal TUI QA journey under `tools/tui-qa/tests/` proving collection member detail, inline expansion under `View → Kind`, and 1-by-1 member install, and update `tools/tui-qa/README.md`.
- [x] 2.3 Run `make validate`, `make spec-check`, and `make tui-qa`.
