## 1. Bind every scenario to its test

- [x] 1.1 Move each `// spec: marketplace-protocols/<slug>` annotation in `pkg/harness-compilation/artifact/{canonical,collection}_test.go`, `pkg/marketplace/api/{openapi,capabilities}_test.go`, `pkg/marketplace/analyze/codes_test.go`, and `internal/config/marketplace_test.go` to the `marketplace-api/<slug>` identity from the design's slug map; comment lines only, no test-logic change; verify `make spec-check` reports no dangling annotation and no unproven scenario
- [x] 1.2 Add `// spec:` annotations for the three new scenarios onto the existing passing tests named in the design (`inspection-content-routes-serve-the-public-media-types`, `capabilities-document-declares-acquisition-modes-and-server-features`, `well-known-document-exposes-browser-templates-only-when-configured`); verify `make spec-check` passes

## 2. Absorb the harvest ledger rows

- [x] 2.1 Check off rows 2.3, 2.7, 2.9, and 4.7 in `openspec/changes/harvest-legacy-docs/tasks.md` and delete `docs/adr/0002-separate-marketplace-identity-from-installed-names.md`, `docs/adr/0005-use-content-addressed-collection-manifests-and-selection-edges.md`, `docs/adr/0007-require-immutable-inspection-projections.md`, and `docs/plans/2026-08-18-marketplace-serve-design.md`; verify the four files are gone and no other ledger row changed
- [x] 2.2 Remove the two links in `docs/plans/2026-09-07-unified-resource-config-design.md` that point at the deleted ADRs; verify no surviving file links to a deleted document

## 3. Verify the rebaseline

- [x] 3.1 Run `openspec validate baseline-marketplace-api --strict` and `make spec-check`; then run the touched packages' tests (`make test TEST_PACKAGES="./pkg/marketplace/api ./pkg/marketplace/analyze ./pkg/harness-compilation/artifact ./internal/config"`) and verify they pass unchanged

## 4. Archive

- [x] 4.1 Run `openspec archive baseline-marketplace-api --yes`, delete any empty `openspec/specs/marketplace-protocols/` directory it leaves, and verify `make spec-check` passes with no `marketplace-protocols` scenario or annotation left outside `openspec/changes/archive`
