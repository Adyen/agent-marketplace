## 1. Align current contracts

- [x] 1.1 Audit current specs and active deltas; record the shared v4-only scope without rewriting historical archives or unrelated schemas.
- [x] 1.2 Update authored reads, settings, Marketplace identity, installation, and TUI startup requirements to use `consumer-config-format`.
- [x] 1.3 Replace legacy collection/required-selection claims with source-bound v4 scenarios and correct active harvest/gate guidance.

## 2. Prove and review

- [x] 2.1 Add or update test bindings proving every modified scenario, including v4 required all/subset/policy-only controls and legacy-key refusal.
- [x] 2.2 Run focused consumer checks, `make validate`, `make validate-e2e` (including the collection witness), `make tui-qa`, and strict spec/traceability checks.
- [x] 2.3 Run OCR delegation-mode adversarial self-review and resolve actionable findings.

## 3. Archive

- [x] 3.1 Run `openspec archive align-v4-only-consumer-specs --yes` and verify `make spec-check`.
