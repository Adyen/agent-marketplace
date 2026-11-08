## 1. Regression Coverage

- [x] 1.1 Add analyzer and ingest regressions and observe the current bypass.

## 2. Implementation

- [x] 2.1 Add the default-reject finding and explicit binary signature exemption.
- [x] 2.2 Update the finding catalog and analyzer version.

## 3. Verification

- [x] 3.1 Run focused tests, benchmark, make validate, spec-check, and E2E.
- [x] 3.2 Review all changes with open-code-review-delegate.
- [x] 3.3 Archive the completed change and verify specification traceability.

## Verification Notes

- The initial analyzer regressions failed for all six resource kinds: invalid
  UTF-8 removed the hidden-content findings and left the resource accepted.
  Mirror and index-only ingest regressions likewise observed an `allow`
  assessment beside metadata-only inspection before the fix.
- Passed fresh tests for analyze, ingest, artifact, and inspection; worker, API,
  and Marketplace client tests also passed.
- Passed `make validate` (selected root packages, vet, lint, and build),
  `make spec-check`, and the declarative E2E spec-tag guard.
- Passed `make validate-e2e`: 251 declarative cases, none skipped, including
  `marketplace/reject_non_utf8_support`; 3571 CLI invocations covered all 114
  requirements, and all four required acceptance witnesses ran and passed.
  The combined gate also passed configuration compatibility and shellcheck.
- Integrated `origin/main` at `60acbcf5` without changing the reviewed
  source/test diff, then repeated `make validate`, `make spec-check`, and
  `make validate-e2e`; all passed again with the same E2E counts.
- The existing in-memory `BenchmarkEngineAnalyzeSkill` measured five 2-second
  samples before and after: median 22874 ns/op before and 23109 ns/op after
  (about 1%); allocations remained 397/op. This benchmark excludes disk and
  network work.
- OCR delegation reviewed commit `d4f057f8`: all four previewed files reviewed,
  none skipped (100% coverage), plus all nine excluded files as supplemental
  context. No actionable findings. The review covered consumer reachability,
  failure behavior, original-byte preservation, inspection restrictions, and
  independent asset checks. Binary-signature recognition is intentionally not
  structural validation or protection against polyglots.
- The archived canonical requirements match the reviewed delta exactly;
  the archive and verification notes received supplemental review.
- The reviewed source/test diff SHA-256 is
  `c98748752837523869c54f23b643ab4090e17a9324c3b620c2944805274b335a`.
