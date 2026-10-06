## 1. Regression coverage

- [x] 1.1 Add and annotate a Playwright test for `marketplace-browser/over-height-desktop-filter-rail-scrolls`; verify it fails before the layout fix because the rail is not viewport-bounded or scrollable.

## 2. Filter rail behavior

- [x] 2.1 Bound the sticky desktop filter rail to the available viewport height and enable native vertical overflow; verify the focused Playwright test passes and mobile behavior is unchanged.

## 3. Verification and archive

- [x] 3.1 Run the web checks, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 3.2 Run `openspec archive fix-scrollable-filter-rail --yes` in this pull request after tests and validation pass; verify the archived `marketplace-browser` spec contains the scenario.
