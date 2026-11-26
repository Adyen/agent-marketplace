# Tasks

## 1. Recovery regression coverage

- [x] 1.1 Add focused e2e-tagged Go recovery coverage for marked regular-file publication, ownership publication, and regular-file prune: inject parent-sync failure, assert the same pending operation and completed count, then allow a retry to finish without repeating the mutation; annotate the test with `// spec: target-profile-place/marked-file-mutation-retries-parent-sync` and verify the regression fails on the base and passes with the fix.
- [x] 1.2 Verify genuine no-ops do not require a parent sync and read-only inspection reports pending work without replay or writes; use the same focused recovery test run to check both controls.

## 2. Recovery durability

- [x] 2.1 Sync the verified effective destination parent before advancing the marked visible-result publication and prune cases, propagate sync errors without advancing progress, and preserve existing generation, identity, and unmarked-absence checks; verify with the focused e2e-tagged recovery tests.

## 3. Integration and completion

- [ ] 3.1 Run `make validate`, `make spec-check`, `make validate-e2e`, and `openspec validate --all --strict`; record successful results for the implementation and scenario binding.
- [ ] 3.2 Archive the completed change with `openspec archive retry-place-recovery-parent-sync --yes` and verify the archived target-profile-place requirement.
