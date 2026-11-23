## 1. Spec

- [x] 1.1 Add `gui-shell` requirement "Sidebar Groups section folds" with the collapsed-by-default scenario.

## 2. Implementation and witnesses

- [x] 2.1 Make the Groups heading a disclosure control in `Sidebar.vue`, collapsed below 720 px window height.
- [x] 2.2 Add a Vitest witness bound to `gui-shell/sidebar-groups-start-collapsed-in-short-windows`.

## 3. Verify

- [x] 3.1 Run the full gui Vitest suite, vue-tsc, and `make spec-check`.
- [x] 3.2 Capture after screenshots at 800x600 and 1024x700.
