## Implementation

- [x] Add command-level regressions for all three scenarios (real `sync` and `deinit --scope user`) and confirm the first two fail on the prior code.
- [x] Compute the machine-scope target set in `pipeline.Resolve` and pass it to the resolver.
- [x] Choose each entry's and Marketplace pin's target set by its owning layer, and each collection member's by the layer that selected the collection, in the resolver; cover it with resolver unit tests.
- [x] Check the machine-scope set in `deinit` before preserving organization output; keep the status probe resolution consistent with `sync`, and report both sets in `status` when they differ.
- [x] Update the configuration reference; run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review adversarially.
- [x] `openspec archive scope-machine-target-harnesses --yes`
