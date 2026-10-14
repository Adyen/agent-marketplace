## 1. Regression evidence

- [x] Prove a whitespace-containing query value and a malformed queued-delivery URL leak through telemetry logs and errors.

## 2. Redaction

- [x] Add structured, fail-closed redaction for values that are exactly one URL.
- [x] Use it for telemetry URL log attributes, error arguments, and URLs embedded in wrapped URL errors.

## 3. Verification

- [x] Run focused tests, make validate, and spec-check.
- [x] Review every changed file using OCR delegation.
- [x] Archive with openspec archive fail-closed-telemetry-url-redaction --yes.
