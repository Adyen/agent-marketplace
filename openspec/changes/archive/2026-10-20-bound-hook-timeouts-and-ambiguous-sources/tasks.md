## Implementation

- [x] Add public `Compile` regressions annotated `// spec: hook-resources/<slug>` for each timeout scenario, and confirm the minimum and exact-range scenarios fail on the prior code.
- [x] Refuse timeouts beyond 2^53-1 in the target's unit, before multiplying for milliseconds, and raise values below a documented per-event minimum.
- [x] Add consumer E2E witnesses with a valid sibling for the per-target exact-range refusal and for an ambiguous hook source.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [x] `openspec archive bound-hook-timeouts-and-ambiguous-sources --yes`
