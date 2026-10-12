## 1. Accepted filter density

- [x] 1.1 Update and annotate component coverage for `marketplace-browser/expansion-action-follows-seven-initial-registries`; verify seven initial options, selected-option visibility, and full expansion.

## 2. Expanded scrollbar presentation

- [x] 2.1 Add and annotate browser coverage for `marketplace-browser/expanded-scrollbar-appears-on-interaction`; verify expanded height, thin width, transparent rest state, and visible hover/focus state.
- [x] 2.2 Apply the accepted viewport-filling height and interaction-only scrollbar styling without JavaScript.

## 3. Verification and archive

- [x] 3.1 Run web checks, the full browser suite, `make validate`, and `make spec-check`; verify all affected gates pass.
- [x] 3.2 Run `openspec archive polish-expanded-filter-scrollbar --yes`; verify the main `marketplace-browser` spec contains the final density and scrollbar contracts.
