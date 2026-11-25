## 1. Spec

- [x] 1.1 Add `gui-shell` requirement "Settings source rows lead with the name" and "Source row remove control on hover and focus".
- [x] 1.2 Modify `gui-shell` requirement "Source search in Settings" to show the field only above six sources, keeping its existing scenarios.

## 2. Implementation and witnesses

- [x] 2.1 Render a named user source's name as the primary line with its location secondary, and sentence-case kind and Organization tags in `SourceList.vue`.
- [x] 2.2 Show the remove control on hover or focus-within, keeping it in the DOM and keyboard reachable.
- [x] 2.3 Show the Settings source search only above six configured sources, and clear the effective query when it is hidden.
- [x] 2.4 Add Vitest witnesses bound to `gui-shell/settings-source-rows-lead-with-the-name`, `gui-shell/source-row-remove-stays-reachable-on-hover-and-focus`, and `gui-shell/source-search-appears-above-six-sources`.

## 3. Verify

- [x] 3.1 Run the full gui Vitest suite, vue-tsc, and `make spec-check`.
- [x] 3.2 Capture an after screenshot of Settings → Sources.
