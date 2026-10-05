## 1. Contract tests and baseline

- [x] 1.1 Add self-contained single-view and row-label tests that fail by assertion on the unchanged TUI, and retain the focused failing command output.
- [x] 1.2 Bind all six new scenarios with test annotations and retain strong provisional, palette, selection, toggle, and synchronization regression assertions.
- [x] 1.3 Capture benchmark baseline using the status-only recompute cases and identical status-tree render fixture; retain actual before terminal captures at common dimensions.

## 2. Single resource view

- [x] 2.1 Remove tab state, switching, alternative trees, and tab layout line; verify Status ordering, fold restoration, search ranking, reload anchoring, minimum dimensions, and inactive former tab keys.
- [x] 2.2 Render fixed-width scope letters before kind and registry/name; verify all badges, blank scope variants, missing registry fallback, omitted subpaths, Unicode, hostile registry text, wrapping, selection, and highlight offsets.
- [x] 2.3 Remove obsolete tab palette/help/footer entries and browse telemetry; verify supported actions and native modal field navigation remain available and detail browse retains its other attributes.

## 3. User journeys and claims

- [x] 3.1 Update real-terminal journeys for single-view browsing, detail inspection, search, folds, and scrolling; verify with make tui-qa-check and make tui-qa and retain equivalent after terminal captures.
- [x] 3.2 Reconcile TUI reference, quick-start, applicable QA reference, and canonical development skill with the implemented behavior; verify removed tab instructions are absent from current user guidance.
- [x] 3.3 Run the same benchmarks after implementation and retain comparison data with rendering-work limitations.

## 4. Integration and archive

- [x] 4.1 Verify the affected TUI package, selected-package native test/build/lint, strict OpenSpec validation and trace audit, and all direct real-terminal journeys, with inspectable logs.
- [x] 4.2 Run openspec archive tui-single-status-view --yes in the same pull request as the implementation and verify synchronized main specs with make spec-check.
