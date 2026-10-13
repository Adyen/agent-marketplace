## 1. Reporting

- [x] 1.1 Add regression tests for multiplicity suppression, strong evidence, mixed directions, and concise collapsed evidence; capture failing TestReport assertions before implementation.
- [x] 1.2 Apply Holm correction across all comparable metrics and update output; pass the scoped report tests and an independently expected Holm arithmetic test.

## 2. Verification

- [x] 2.1 Replay the frozen PR #622 artifact, inspect the output, and review the complete diff without rerunning benchmarks.
- [x] 2.2 Run make validate scoped to ./tools/benchmark-report and make spec-check; preserve user-owned go.work.sum.
- [x] 2.3 Run openspec archive accurate-benchmark-significance --yes after verification.
