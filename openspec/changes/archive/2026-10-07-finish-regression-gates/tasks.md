## 1. Regression evidence

- [x] 1.1 Add an annotated nightly workflow budget regression and demonstrate failure at the old deadline.
- [x] 1.2 Strengthen the annotated released-client consumer test and demonstrate failure with the old release pin.
- [x] 1.3 Add annotated declarative post-migration bulk, tag, and ad-hoc consumer regressions.

## 2. Gate corrections

- [x] 2.1 Preserve 20 iterations and journey assertions while increasing the nightly deadline to 60 minutes.
- [x] 2.2 Pin verified v4.1.1 assets consistently in the fixture and CI; verify successful interoperability and lock preservation.
- [x] 2.3 Update the regression-gate plan to describe the completed implementation and current evidence.

## 3. Verification

- [x] 3.1 Run focused gate tests, the dedicated released-client gate, local validation, and strict spec checks.
- [x] 3.2 Record adversarial review of the verified changes.
- [x] 3.3 Add the approved E2E Gates context without changing other repository rules; verify the resulting ruleset.
- [x] 3.4 Run `openspec archive finish-regression-gates --yes` in this pull request.
