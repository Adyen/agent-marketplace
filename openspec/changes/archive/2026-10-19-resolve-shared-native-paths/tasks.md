## Implementation

- [x] Add a command regression over both source orders and both materialization modes, with a Claude Code control, and confirm it fails on the prior code.
- [x] Key the materialization projection by native path, return displacements across kinds, and report them as `materialize.collision.shared-path`; cover the projection with resolver unit tests.
- [x] Add a benchmark for the projection and record before and after measurements.
- [x] Update the configuration reference; run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review adversarially.
- [x] `openspec archive resolve-shared-native-paths --yes`
