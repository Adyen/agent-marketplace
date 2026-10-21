## 1. Healthcheck Deadline

- [x] 1.1 Observe the missing-deadline regression fail before the fix.
- [x] 1.2 Add the native request deadline without changing shell-free startup.
- [x] 1.3 Run the extracted healthcheck against successful root/subpath
  responses, an HTTP failure, and a stalled response; the stalled request
  exited nonzero after 4.06 seconds.

## 2. Verification

- [x] 2.1 Run CI-plan tests with real Helm rendering.
- [x] 2.2 Address the remaining bot feedback and run scoped validation,
  spec-check, and the final adversarial OCR delegation review.
- [ ] 2.3 Archive the change as the final commit and verify the updated PR.

## Review Decisions

- Gemini: add an explicit four-second request abort before Docker's existing
  five-second process timeout; both success paths, HTTP failure, and a stalled
  response were exercised using the Dockerfile's actual healthcheck code.
- CodeRabbit: document upgrading values that relied on implicit tags and add
  a previous-values fixture. The original chart renders that fixture with
  `latest`; the hardened chart refuses missing versions and accepts explicit
  independent digests.
- CodeRabbit: inspect scanner-outcome conditions after the scanner as well as
  before it. A temporary reintroduction of the removed status-repair step
  failed the regression; unrelated failure diagnostics remain allowed.
- Release/versioning changes are explicitly out of scope by maintainer
  direction. No version bumps or release targets are introduced.
- The speculative module-cache finding is not reproduced: the Helm job has
  read-only repository permissions and PR-scoped cache writes do not create
  a privileged default-branch cache path. Cache policy is unchanged.
