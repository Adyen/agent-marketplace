## 1. Sticky catalog contract

- [x] 1.1 Add and annotate browser coverage for `marketplace-browser/catalog-controls-stick-while-resource-rows-scroll`; verify it fails while catalog controls remain static.

## 2. Catalog control wrapper

- [x] 2.1 Group catalog controls above the result states and make the group sticky at the desktop filter offset; verify the introduction and rows continue normal page scrolling.
- [x] 2.2 Verify active-filter controls and expanded filter scrolling retain their existing behavior.

## 3. Verification and archive

- [x] 3.1 Run web checks, the full browser suite, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 3.2 Run `openspec archive stick-catalog-controls --yes`; verify the main `marketplace-browser` spec contains the sticky-controls scenario.
