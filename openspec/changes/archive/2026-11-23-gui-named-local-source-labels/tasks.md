## 1. Spec

- [x] 1.1 Add `gui-shell` requirement "Named local source labels" with the named-source and unnamed-fallback scenarios. The Marketplace endpoint requirement is unchanged.

## 2. Implementation and witnesses

- [x] 2.1 Label named directory and Git sources by their configured name in `sourceLabel`, and reuse the same check for the sidebar row and group label.
- [x] 2.2 Add Vitest witnesses bound to `gui-shell/named-local-source-shows-its-configured-name` and `gui-shell/unnamed-local-source-keeps-its-path-label`.

## 3. Verify

- [x] 3.1 Run the full gui Vitest suite, vue-tsc, and `make spec-check`.
- [x] 3.2 Capture an after screenshot of the sidebar with named sources.
