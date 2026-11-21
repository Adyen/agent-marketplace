## 1. Spec

- [x] 1.1 Retire `gui-shell` "Sidebar source labels" and add "Sidebar Marketplace endpoint labels" so Marketplace sources are labeled by endpoint regardless of name.

## 2. Implementation and witnesses

- [x] 2.1 Make `marketplaceLabel` in `gui/src/source-label.ts` ignore the configured name and drop the generic-name pattern.
- [x] 2.2 Update `gui/src/source-label.test.ts` and the sidebar label test in `gui/src/sources.test.ts` to bind `gui-shell/marketplace-source-shows-its-endpoint-regardless-of-name`, and remove the retired scenario annotations everywhere.
- [x] 2.3 Remove the retired scenario annotations from `internal/localapi/configuration_test.go`.

## 3. Verify

- [x] 3.1 Run the full GUI vitest suite, `vue-tsc --noEmit`, and `make spec-check` from the repository root.
