## Implementation

- [x] Add public regressions for both scenarios and confirm they fail on the prior code.
- [x] Omit the zeroed `Extra` member and a null `Extra` extension field in the canonical encoder; leave decoding unchanged.
- [x] Build the earlier payload shape explicitly in the existing Marketplace API hook-metadata tests, and update the changelog.
- [ ] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [ ] `openspec archive drop-zeroed-extra-from-resource-payloads --yes`
