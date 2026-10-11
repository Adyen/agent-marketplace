## 1. Regression Coverage

- [x] 1.1 Add failing mixed-rule adoption and search-consumer regressions with bindings for `marketplace-api/rule-observations-preserve-selectable-resource-adoption`; include valid skill control and malformed kind/value controls, and verify the regressions fail before implementation and pass afterward.
- [x] 1.2 Add an independently authored deterministic query regression and verify the lead-run real HTTP synthetic-vector evaluation with frozen-result offline reader coverage for `marketplace-api/superseded-inventory-history-does-not-exhaust-the-response-budget`; include tied generations, empty latest inventory, stale/future values, duplicate series, deployment isolation, and existing oversized fallback, and verify the evaluator result and regression tests.

## 2. Implementation

- [x] 2.1 Apply the lead-authored latest-generation query projection and validated-rule exclusion without changing response/time bounds, origin matching, caching, or public schemas; verify these unchanged constraints and the focused API regressions.

## 3. Verification and Completion

- [x] 3.1 Run focused API tests, narrow before/after benchmarks, one final scoped `make validate` and spec checks; review the full diff and retain inspectable evidence, verifying each requested command and artifact is recorded.
- [x] 3.2 Run `openspec archive fix-installation-ranking-inventory --yes` in the same pull request as implementation; verify the archive completes successfully.
