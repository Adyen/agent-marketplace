## 1. Regression and behavior

- [x] 1.1 Add command tests with spec annotations for reconciled nested output, drifted nested output, and root/exact-lock preservation; record the red run of the nested regression on the original base fe4c9a7a (false drift at the repository root).
- [x] 1.2 Confirm upstream `pipeline.NewWorkspace`/`pipeline.Inspect` workspace inspection makes status preview the effective nested output root so the rebased branch carries no production change; verify focused command tests pass and status leaves output and lock bytes unchanged.
- [ ] 1.3 Run `make validate`, `make spec-check`, `make validate-e2e`, and `openspec validate --all --strict`; verify all checks pass.

## 2. Publication

- [ ] 2.1 Run `openspec archive fix-nested-status-drift --yes` in the same pull request; verify the status-reporting main spec contains the new scenarios.
