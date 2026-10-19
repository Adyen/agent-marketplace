## Implementation

- [x] Reproduce #781 with a command test and an E2E table case that fail at `ce1774b6`.
- [x] Keep the outranking required member of a case-only group in the projection, and cover requirement ties, layer precedence and both input orders with resolver tests.
- [x] Name the required winner in the `materialize.collision.shared-path` warning; update the catalog, CHANGELOG and configuration reference.
- [x] Run benchmarks, affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review adversarially.
- [x] `openspec archive keep-required-in-case-only-collisions --yes`
