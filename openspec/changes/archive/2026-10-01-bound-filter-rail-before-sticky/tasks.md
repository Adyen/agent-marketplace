## 1. Pre-sticky filter scrolling

- [x] 1.1 Restore and annotate browser coverage for `marketplace-browser/filter-rail-stays-visible-before-becoming-sticky`; verify it fails with the sticky-offset height calculation.
- [x] 1.2 Apply a conservative CSS-only maximum height; verify the rail bottom and final control remain visible while the resource list stays at its initial position.

## 2. Verification and archive

- [x] 2.1 Run web checks, browser tests, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 2.2 Run `openspec archive bound-filter-rail-before-sticky --yes`; verify the main `marketplace-browser` spec contains the pre-sticky scenario.
