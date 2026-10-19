## Implementation

- [x] Reproduce the case-variant replacement on a case-insensitive APFS volume, and add a command regression that fails on the prior code on any host.
- [x] Compete case variants across kinds on shared paths in the projection, keeping same-kind case variants for the case-only refusal; cover both precedence directions with resolver tests.
- [x] Omit displaced targets from `status` rows, with a unit test that fails on the prior code.
- [x] Run affected packages, benchmarks, `make validate`, `make spec-check` and `make validate-e2e`; review adversarially.
- [x] `openspec archive resolve-case-variant-shared-paths --yes`
