## Implementation

- [x] Add failing regression coverage for bulk selectors in all writable scopes.
- [x] Enable bulk scopes using the shared scoped editor and registry precedence.
- [x] Prove directory-source scope boundaries and personal-local subdirectory behavior.
- [x] Add end-to-end scope journeys and update the CLI reference.
- [x] Run focused tests, validation, specification gates, and adversarial OCR delegation review.
- [x] Run `openspec archive enable-bulk-install-scopes --yes`.

## Verification

- The initial bulk editor regression run failed on all eight newly supported selector/scope combinations with the existing repo-only errors.
- Focused command/pipeline tests and all eight E2E journeys passed, including user-only source declarations without repository configuration.
- `make validate` passed the full root short-mode selection before rebase. After rebase, `make validate TEST_PACKAGES='./cmd/agent-marketplace ./internal/pipeline'` passed both complete selected packages, lint, and build.
- `make validate-e2e` passed: 2,907 invocations, all 113 command requirements covered, no listed gaps, all three required refusal witnesses passed, and shellcheck passed.
- `make spec-check` passed after archival: all 59 specification items, 990 scenarios with 1,597 annotations, and the archive-before-merge check.
- OCR delegation review of implementation `879e95b5`: all 12 changed files host-reviewed (two OCR-selected and ten supplemental), zero skipped, no actionable findings. Review covered consumer reachability, failure publication, scope boundaries, exact pins, and regression validity; passing checks do not prove the absence of defects.
- The archive and canonical specification diff were also reviewed: final coverage is 13 of 13 changed files, zero skipped, no actionable findings.
