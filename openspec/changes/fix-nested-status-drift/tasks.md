## 1. Regression and behavior

- [ ] 1.1 Add command tests with spec annotations for reconciled nested output, drifted nested output, and root/exact-lock preservation; verify the nested regression fails on the base commit for false drift.
- [ ] 1.2 Make status preview the effective nested output root without changing synchronization; verify focused command tests pass and status leaves output and lock bytes unchanged.
- [ ] 1.3 Run `make validate`, `make spec-check`, `make validate-e2e`, and `openspec validate --all --strict`; verify all checks pass.

## 2. Publication

- [ ] 2.1 Run `openspec archive fix-nested-status-drift --yes` in the same pull request; verify the status-reporting main spec contains the new scenarios.
