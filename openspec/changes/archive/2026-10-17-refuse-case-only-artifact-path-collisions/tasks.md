## Implementation

- [x] Add public regressions for both scenarios (library `Convert` and a command-level `sync`) and confirm they fail on the prior code.
- [x] Compare bundle artifact paths case-insensitively and name both paths in the conflict message.
- [x] Refuse case-only name collisions in the installation work loop before any write.
- [x] Update the changelog; run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; reproduce on a case-insensitive volume with a real `sync`; review adversarially.
- [x] `openspec archive refuse-case-only-artifact-path-collisions --yes`
