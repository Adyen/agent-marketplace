## 1. Inventory identity

- [x] 1.1 Add failing mixed hosted/Git inventory, canonical-name, malformed-identity and shared-resolution export regressions; preserve red output.
- [x] 1.2 Implement bounded hosted inventory identity and detailed-only OTLP attributes; verify inventory and telemetry regressions, legacy fingerprints and the inventory hot-path benchmark.

## 2. Adoption

- [x] 2.1 Preserve hosted labels in the latest-inventory query and isolate aggregation by Marketplace instance; verify pinned query, hosted/Git deduplication, foreign-instance, freshness, ambiguity and malformed-pair tests.
- [x] 2.2 Prove hosted counts through the public installation-ordered search API and update existing Git-only published descriptions; verify public-search regressions without changing response shape.

## 3. Ingestion and delivery

- [x] 3.1 Deliver the lead-authored additive collector filter/allowlist in the separate k8s-multitenant branch; validate the cached collector configuration, valid legacy/hosted and malformed OTLP inputs, and rendered Kustomize overlay. No production rollout.
- [x] 3.2 Review the complete diffs and run one final scoped make validate and make spec-check; retain evidence and report rollout ordering and any blockers.
- [ ] 3.3 Run openspec archive fix-hosted-marketplace-inventory --yes as the final implementation artifact update and verify make spec-check against the archived delta.
