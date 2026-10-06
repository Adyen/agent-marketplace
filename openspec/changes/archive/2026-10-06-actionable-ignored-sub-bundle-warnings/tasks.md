## 1. Update and regressions

- [x] 1.1 Merge main and reproduce the ignored-bundle scanner's captured-source regression.
- [x] 1.2 Retain the existing captured source root in the scanner and isolate the remote context test's HOME.
- [x] 1.3 Bind the mixed-root, registry-context, sorted/bounded, and captured-source scenarios to tests.

## 2. Validation and delivery

- [x] 2.1 Run affected catalog and consumer tests, benchmark the discovery path, and run validation and spec checks.
- [x] 2.2 Review the complete intended PR delta and record the outcome before pushing.

## 3. Archive

- [x] 3.1 `openspec archive actionable-ignored-sub-bundle-warnings --yes`

## Evidence

- The captured-source regression reported replacement-root content before the scanner correction, then passed with the captured source.
- Catalog tests and focused pipeline, CLI, and TUI consumer regressions passed.
- `make validate`, a fresh-cache `make ci-lint`, `make spec-check`, and the spec delta gate passed.
- `make validate-e2e` passed the combined E2E/configuration-compatibility suite and shellcheck.
- Mixed-root discovery measured 23.59 ms/op before and 23.89 ms/op after, with no significant timing difference (five 2-second samples each). Memory changed from 526.1 KiB/op to 522.2 KiB/op and allocations from 10,184 to 10,140 per operation.
- Adversarial review of the intended delta against main at `45ebb6fa` covered consumer reachability, failure behavior, terminal quoting and URL redaction, unintended writes, captured-source authority, and regression validity. No further actionable findings.
