## Implementation

- [x] Add a command regression with a version-ref control and confirm it fails on the prior code.
- [x] Add `giturl.IsFullObjectID` with unit coverage and skip full object IDs in `bumpRepoRegistryRefs` before the fetch.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review adversarially.
- [x] `openspec archive keep-commit-pins-out-of-ref-bumping --yes`
