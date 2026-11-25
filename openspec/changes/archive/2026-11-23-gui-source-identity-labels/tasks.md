## 1. Spec

- [x] 1.1 Add `gui-shell` requirement "Source identity labels match the sidebar" with its scenario.

## 2. Implementation and witnesses

- [x] 2.1 Use the shared source display label for the filter chip, browse header title, and inspector Registry value.
- [x] 2.2 Show the shortened browse header location with the full location in its title.
- [x] 2.3 Add Vitest witnesses bound to `gui-shell/source-identity-labels-match-the-sidebar`.

## 3. Verify

- [x] 3.1 Run the full gui Vitest suite, vue-tsc, the source Playwright specs, and `make spec-check`.
- [x] 3.2 Capture an after screenshot of the browse header and inspector.
