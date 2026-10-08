## 1. Named batch installation

- [x] 1.1 Add annotated command regressions for every resource-installation scenario and observe focused multi-name tests fail against the base.
- [x] 1.2 Accept multiple ordinary names while rejecting incompatible Marketplace and selector combinations; verify argument-validation regressions.
- [x] 1.3 Resolve and apply the complete ordered batch through shared install selection, retaining collection semantics and scope checks; verify configured and explicit-source success, deduplication, exclusion cleanup, and warning filtering tests.
- [x] 1.4 Prove invalid later names leave existing or absent configurations, acquisition locks, exclusions, and materialization unchanged; verify failure matrices through the command boundary.

## 2. Integration and published behavior

- [x] 2.1 Add a declarative Git-source multi-name E2E case and verify exact ref/path selection, materialization, unrelated-resource preservation, and subsequent sync check.
- [x] 2.2 Update install usage in the CLI reference and reconcile it against the implemented command and spec.
- [x] 2.3 Run focused install regressions, make validate, make spec-check, openspec validate --all --strict, and make validate-e2e; retain passing output.
- [x] 2.4 Run openspec archive install-multiple-resources --yes in the same pull request and verify the archived change and main capability with make spec-check.
