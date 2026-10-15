## 1. Regression tests

- [x] 1.1 Add `TestErrorRecoveryToggleReloadKeepsSharedKeyCategories` for a toggle reload whose snapshot materialization cause and operation cause share a stable key
- [x] 1.2 Add `TestErrorRecoverySharedKeySeparatesSeverities` for a snapshot warning and an operation editor error sharing a stable key
- [x] 1.3 Record RED output before production changes

## 2. Rendering fix

- [x] 2.1 Key recovery categories by severity and stable key in `diagnosticCategories`
- [x] 2.2 Consume categories in display order through a `renderErrors` closure for errors and warnings

## 3. Verification

- [x] 3.1 Benchmark `BenchmarkRenderErrorsRecovery` before and after the change
- [x] 3.2 Focused and `TestErrorRecovery` prefix tests pass
- [x] 3.3 `make validate TEST_PACKAGES=./internal/tui` passes
- [x] 3.4 `make spec-check` passes or is reported blocked pending archive
- [x] 3.5 `make tui-qa` passes
