## Implementation

- [x] Add a public regression for the scenario and confirm it fails on the prior code.
- [x] Compare bundle artifact paths case-insensitively and name both paths in the conflict message.
- [ ] Update the changelog; run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; reproduce on a case-insensitive volume with a real `sync`; review adversarially.
- [ ] `openspec archive refuse-case-only-artifact-path-collisions --yes`
