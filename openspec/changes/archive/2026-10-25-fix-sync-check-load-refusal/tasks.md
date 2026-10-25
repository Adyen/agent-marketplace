## Implementation

- [x] Enable and strengthen the unloadable-registry witness with both scenario bindings, both scopes, native-file/state preservation, and valid-source controls.
- [x] Observe the regression fail before changing production behavior.
- [x] Repair the local-registry benchmark fixtures and capture a before measurement.
- [x] Enable strict pre-materialization refusal for synchronization checks and verify focused regressions and benchmark results.
- [x] Review consumer reachability, refusal behavior, scope/ownership boundaries, and regression validity with adversarial and OCR delegate review.
- [x] Run scoped `make validate` and rerun timed-out command scenarios serially.
- [x] Run `make spec-check` before and after archival.
- [x] Pass the mandatory `make validate-e2e` gate.
- [x] Run `openspec archive fix-sync-check-load-refusal --yes`.
