## Implementation

- [x] Add failing linked-resource and support-content regressions and capture a check benchmark baseline.
- [x] Compare desired cache content without writes in both linked-resource paths.
- [x] Add a CLI E2E regression proving drift, no writes and successful repair.
- [x] Run focused tests, benchmarks, `make validate`, `make validate-e2e` and adversarial review.
- [ ] Run `openspec archive fix-symlink-content-drift --yes` and `make spec-check`.
