## 1. Browser contracts

- [x] 1.1 Replace the existing desktop rail regressions with annotated scenarios for `marketplace-browser/collapsed-filter-rail-follows-the-resource-list` and `marketplace-browser/expanded-filter-rail-scrolls-to-final-registry`; verify they fail with unconditional overflow.
- [x] 1.2 Expand the E2E registry fixture enough to exercise a long expanded list without changing search results.

## 2. Expanded-only scrolling

- [x] 2.1 Expose the existing expansion state as a data attribute and apply desktop overflow only to the expanded filter component; verify collapsed and expanded browser scenarios pass.

## 3. Verification and archive

- [x] 3.1 Run web checks, the full browser suite, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 3.2 Run `openspec archive scroll-expanded-filters-only --yes`; verify the main `marketplace-browser` spec contains only the final collapsed and expanded scrolling contracts.
