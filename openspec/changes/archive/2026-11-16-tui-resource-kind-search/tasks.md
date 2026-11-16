# Tasks

## 1. Kind-aware discovery

- [x] 1.1 Add failing table-driven client/model regressions binding `tui-navigation/every-kind-query-discovers-all-marketplace-matches` and `tui-navigation/other-kinds-retain-additive-text-matching`; cover every supported resource kind, with opaque-name hosted matches beyond 50, multiple endpoints, mixed case, and preserved text ranking. Explicitly verify both `mcp` and `command` use the same generic behavior.
- [x] 1.2 Implement additive kind-aware TUI discovery and required pagination through existing validated client requests; verify the new regressions and affected package tests pass without changing CLI search or installation.
- [x] 1.3 Add failing regressions binding `tui-navigation/superseded-kind-search-cannot-replace-current-results` and `tui-navigation/failed-kind-discovery-preserves-usable-local-results`; verify late query results, no matches, clear, endpoint policy/failure, and unchanged consumer state through the real model/client flow.
- [x] 1.4 Capture before/after search/discovery benchmarks under repository standards and retain the benchmark; verify latency and allocation measurements include the changed hot paths.
- [x] 1.5 Extend `tools/tui-qa/tests/search-ranking.spec.ts` or add a focused Marketplace kind-search journey; prove `/mcp` reaches opaque-name hosted MCPs and `/command` reaches opaque-name hosted commands, clears correctly, and fails on the unfixed base. Register it in the QA README and update `site/docs/reference/tui.md`; verify with `make tui-qa`.

## 2. Integration checks

- [x] 2.1 Bind every new scenario with `// spec:` annotations, run `make validate && make spec-check && make tui-qa`, and verify all required journeys execute with none skipped. Add `make validate-e2e` if changes reach shared consumer behavior.

## Workflow follow-up

- Run `openspec archive tui-resource-kind-search --yes` as the final task and commit in the implementation pull request; verify archived specs retain the scenarios and `make spec-check` passes.
