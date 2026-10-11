## Context

The October 7 nightly run starts iteration 16 before GitHub cancels it at the 30-minute job deadline. Completed iterations take about 113 seconds, so 20 iterations require roughly 38 minutes plus setup. A 60-minute job deadline retains bounded execution without weakening journey assertions, retries, or iteration count.

The latest stable release is v4.1.1. Its Linux assets have GitHub-published SHA-256 digests; downloads must match those exact values before execution. Historical executables remain confined to the existing network-disabled, read-only Docker fixture.

## Decisions

- Raise only the nightly job deadline to 60 minutes. Preserve all 20 iterations, per-journey timing ceilings, and fail-fast behavior.
- Reuse the existing released-client fixture and independently authored Git resource contents. Give each teammate a separate checkout and HOME, exchanging actual Git commits without generated output or cache-backed symlinks. Successful `sync --locked` and `sync --check`, not expected rejection, are the interoperability oracle.
- Prove the remaining post-migration bulk, tag, and ad-hoc workflows with declarative built-binary cases, not constructor or fresh-v4-only checks.
- Assert lock byte preservation around plain candidate sync and each released-client read. Assert actual updated content and removal so successful exits cannot hide missing mutations.
- Preserve every existing default-branch rule and check binding when adding the GitHub Actions `E2E Gates` context.

## Validation

Demonstrate the nightly budget regression and strengthened interoperability test fail before the configuration fixes. Then run scoped workflow/witness tests, the verified released-client Docker gate, the required local validation gates, and pre-push adversarial review. Merge and issue closure require successful PR checks; a full scheduled/manual 20-iteration run provides nightly completion evidence.

### Executed checks

- Both negative controls failed at their intended assertions before the fixes.
- Workflow/planner/witness tests and all three post-migration lifecycles passed.
- Checksum-verified v4.1.1 released-client Docker gates passed for arm64 and amd64; all five pinned v3.1.1 upgrade scenarios ran and passed.
- `make validate` and fresh-cache lint passed with zero issues.
- The combined host E2E/configuration suite and shellcheck passed. All three Node process tests and all 18 real-terminal journeys passed.
- The first concurrent `make validate-full` run hit the existing 120-second command-test deadline in `TestRemoveBlockedManagedResourcePreservesSiblings`. Its isolated rerun passed in 28 seconds. A standalone fresh witnessed root-suite rerun with four child slots then passed, with all 70 behavior witnesses executed. The initial aggregate failure is not counted as a pass.
- Strict OpenSpec validation and traceability passed before archive; final archived-spec checks run in the archive commit.
- The adversarial delegate reviewed all 11 intended entries at staged diff SHA-256 `dbe55c81ae2e11c09b99d528487ab3e3fcda08ddf0ea0b947910fd3eefcf6749` (implementation commit `4994990c`). No critical, high, or medium finding remained; the stale task-checkbox documentation finding is corrected.
- The approved default-branch ruleset update was read back and compared after removing the added context: all prior rules, check bindings, bypass settings, and conditions were unchanged.

No production hot path changed. The newly provisioned 60-minute nightly deadline has not yet been exercised by a full 20-iteration run in this branch.
