## 1. Regression coverage

- [x] 1.1 Add and annotate a Playwright test for `marketplace-browser/filter-rail-stays-visible-before-becoming-sticky`; verify it fails because the rail extends below the viewport at the top of the catalog.

## 2. Viewport sizing

- [x] 2.1 Measure the rail's current viewport position and update its CSS height input on page scroll and viewport resize; verify the focused regression and existing responsive suite pass.

## 3. Verification and archive

- [x] 3.1 Run web checks, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 3.2 Run `openspec archive keep-filter-rail-in-viewport --yes`; verify the main `marketplace-browser` spec contains both scenarios.
