## 1. Regression evidence

- [x] Prove credentials reach logs and errors through configuration validation, route clients, environment overrides, queued delivery, and `telemetry enable`.
- [x] Prove the shared redaction policy drops the scheme and leaves the query visible for a credentialed URL with an explicit port.

## 2. Redaction

- [x] Add an identity-preserving redacted error wrapper beside the shared URL redaction policy.
- [x] Fix the shared SCP-credential pattern so it cannot consume a URL scheme.
- [x] Redact telemetry URL log attributes and error text at their sources.

## 3. Verification

- [x] Run focused tests, make validate, and spec-check.
- [x] Review every changed file using OCR delegation.
- [x] Archive with openspec archive redact-telemetry-endpoint-credentials --yes.
