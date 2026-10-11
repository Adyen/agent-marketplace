## 1. Collapsed registry filters

- [x] 1.1 Add and annotate component coverage for `marketplace-browser/expansion-action-follows-five-initial-registries`; verify it fails while six initial registries remain visible.
- [x] 1.2 Reduce the collapsed registry limit to five; verify selected registries remain visible and expansion still shows all registries.

## 2. Verification and archive

- [x] 2.1 Run web checks, browser tests, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 2.2 Run `openspec archive surface-filter-expansion-action --yes`; verify the main `marketplace-browser` spec contains the collapsed-filter contract.
