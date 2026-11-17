# Tasks

## 1. Complete background inventory

- [x] 1.1 Add failing client/model tests binding `tui-startup/cold-startup-browses-beyond-the-first-page` and `tui-startup/warm-startup-and-empty-endpoints-remain-usable`; cover more than 50 resources, multiple delayed endpoints, warm/stale cache, an empty endpoint, installed deduplication, and one equivalent load across startup stages.
- [x] 1.2 Extend the existing discovery pagination/cache path and progressive startup integration; verify every resource arrives without a query, input works before all pages complete, and a fresh first-page cache cannot suppress remaining pages.
- [x] 1.3 Measure fresh-process cold/warm startup, time to first useful frame and acknowledged navigation, request counts, and peak memory with isolated local multi-page endpoints; retain relevant benchmarks and before/after results including disk/network work.
- [x] 1.4 Add a real-terminal startup-browsing journey directly under `tools/tui-qa/tests/`; verify navigation during a delayed later page and reachability beyond page one without searching. Register it and document background browsing in the TUI reference; run `make tui-qa`.

## 2. Search continuity and failure isolation

- [x] 2.1 Add failing tests binding `tui-startup/search-during-loading-and-clear-restore-complete-browsing` and `tui-startup/superseded-browse-results-cannot-restore-removed-endpoints`; verify overlapping search, clear, selection/detail identity, folds, reload, and removed-endpoint late responses.
- [x] 2.2 Implement browse/query lifecycle isolation and equivalent-load coalescing; verify those regressions pass and extend the terminal journey to search during loading and clear back to the complete list.
- [x] 2.3 Add tests binding `tui-startup/offline-partial-and-denied-discovery-preserve-useful-browsing`; verify failed later pages retain received rows, incomplete/error feedback, no denied requests, quit cancellation, no idle polling, and unchanged consumer config/locks/materialized files.

## 3. Integration checks

- [x] 3.1 Bind every new scenario with `// spec:` annotations, run `make validate && make spec-check && make tui-qa`, and verify all required journeys execute with none skipped. Add `make validate-e2e` if shared consumer behavior changes.

## Workflow follow-up

- Run `openspec archive tui-marketplace-startup-browse --yes` as the final task and commit in the implementation pull request; verify archived specs retain the scenarios and `make spec-check` passes.
