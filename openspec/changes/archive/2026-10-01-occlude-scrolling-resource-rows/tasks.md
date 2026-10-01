## 1. Occlusion regression

- [x] 1.1 Add and annotate browser coverage for `marketplace-browser/sticky-catalog-controls-occlude-resource-rows`; verify scrolling row content currently wins hit testing behind controls or in the navigation gap.

## 2. Sticky backdrop

- [x] 2.1 Raise the catalog-control stacking layer and add an isolated opaque backdrop through the navigation gap; verify rows remain hidden while scrolling.

## 3. Verification and archive

- [x] 3.1 Run web checks, the full browser suite, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 3.2 Run `openspec archive occlude-scrolling-resource-rows --yes`; verify the main `marketplace-browser` spec contains the occlusion scenario.
