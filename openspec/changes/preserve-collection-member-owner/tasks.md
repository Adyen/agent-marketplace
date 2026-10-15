## 1. Specify and reproduce

- [x] 1.1 Add annotated resolver regressions for retained owner scope, required policy, and independent scope copies; confirm failures before the fix.
- [x] 1.2 Add annotated declarative E2E cases with v4 consumer roots and verify native output destinations and drift.
- [x] 1.3 Measure the existing cross-scope collection resolver benchmark before implementation.

## 2. Fix and verify

- [x] 2.1 Remove the unqualified member's redundant owner lookup without changing explicit registry references or nested definition selection.
- [x] 2.2 Run focused regressions, affected package tests, and the post-change benchmark.
- [x] 2.3 Run validation, the combined E2E gate, and strict specification validation; complete adversarial OCR delegation review.

## 3. Archive

- [ ] 3.1 Run `openspec archive preserve-collection-member-owner --yes` as the final commit of the implementation.
