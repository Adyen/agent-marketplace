## 1. GC safety and evidence

- [ ] 1.1 Add red tests for interrupted-write residue, malformed completed metadata, and bounded oversized metadata, then make focused Marketplace cache tests pass.
- [ ] 1.2 Add red tests for unchanged-input dry-run parity and changed configuration or lock while GC waits on its lease, then make focused pipeline and command tests pass.

## 2. Inspection parity

- [ ] 2.1 Add red command and TUI tests for shared malformed-resource severity and source paths, plus named and root-level materialization diagnostics; make focused consumer tests pass.

## 3. Verification and publication

- [ ] 3.1 Run focused tests and `make validate`, `make spec-check`, `make validate-e2e`, and `make tui-qa`; record pass or exact blockers.
- [ ] 3.2 Reconcile PR #275 benchmark and verification claims against measured evidence and verify the PR body.
- [ ] 3.3 Run `openspec archive pr275-gc-inspection-followup --yes` in the same pull request and verify `make spec-check`.
