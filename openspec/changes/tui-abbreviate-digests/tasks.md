## 1. Implementation

- [ ] 1.1 Add a helper in `internal/tui/detail.go` to abbreviate algorithm-qualified hex digests (`<algo>:<first 12 hex>…`) without altering non-digest revisions or underlying row data.
- [ ] 1.2 Update `renderDetail` in `internal/tui/detail.go` to display abbreviated digests and skip the `Revision:` row when `Revision == ContentDigest`, while keeping full digests in `renderMarketplaceDetailContent`.

## 2. Verification

- [ ] 2.1 Add spec-annotated Go tests in `internal/tui/detail_test.go` and `internal/tui/marketplace_detail_test.go`.
- [ ] 2.2 Add or extend a real-terminal TUI QA journey under `tools/tui-qa/tests/` verifying abbreviated summary digests vs. full detail view, and update `tools/tui-qa/README.md`.
- [ ] 2.3 Run `make validate`, `make spec-check`, and `make tui-qa`.
