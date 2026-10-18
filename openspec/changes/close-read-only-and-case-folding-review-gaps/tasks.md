## Implementation

- [x] Add regressions (Codex read-only sandbox to Claude, Cursor and Factory; sigma and final sigma paths) and confirm they fail on the prior code.
- [x] Let a Codex read-only sandbox through the native-loss guard on targets with a read-only mode.
- [x] Use Unicode simple case folding for artifact-path collision keys.
- [x] Use independent expectations in the fallback digest test.
- [ ] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review adversarially.
- [ ] `openspec archive close-read-only-and-case-folding-review-gaps --yes`
