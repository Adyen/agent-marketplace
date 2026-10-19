## 1. CSS simplification

- [x] 1.1 Remove runtime filter-rail measurement and use fixed CSS offsets below the sticky header; verify the focused sticky-rail browser test passes.
- [x] 1.2 Remove the superseded pre-sticky browser scenario and verify spec traceability passes.

## 2. Verification and archive

- [x] 2.1 Run web checks, the full browser suite, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 2.2 Run `openspec archive simplify-filter-rail-css --yes`; verify the main `marketplace-browser` spec contains the fixed-offset contract.
