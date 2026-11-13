## 1. Planner core (B0)

- [x] 1.1 Add the repair diagnosis, step model, planner, and re-diagnose loop with the recurrence cap; verify with focused `internal/repair` tests
- [x] 1.2 Wire `doctor --repair` to print the numbered plan, ask the plan-level confirmation, honor `--yes` and off-terminal preview, and report exit status; verify with command tests annotated for the `doctor-repair-planning` confirmation, non-interactive, and exit-status scenarios
- [x] 1.3 Degrade per step on invalid configuration, failed resolution, and organization acquisition failure; verify with tests annotated for the degrade scenarios and the existing organization acquisition E2E test
- [x] 1.4 Revalidate policy before every confirmed step and at publication; verify with a test annotated for the policy-change scenario and the existing repair policy tests
- [x] 1.5 Add the harness drift stage through sync without acquisition-lock writes; verify with a test annotated for the drift scenario that asserts lock bytes and a clean sync check
- [x] 1.6 Run the Devin and Codex shared-aggregate repair as the final stage; verify the existing `doctor_*_test.go` and `e2e/doctor_repair_policy_test.go` pass unmodified and a test annotated for the stage-order scenario passes
- [x] 1.7 Update the `doctor` command help for the planner; verify `make spec-check` and the command-path witness test pass

## 2. Journal recovery fixer (B1)

- [ ] 2.1 Plan and run dependency and ownership journal recovery as a safe stage-1 step; verify with annotated tests

## 3. Acquisition lock fixers (B2)

- [ ] 3.1 Add safe lock refresh and snapshot fetch steps, destructive conflicted-lock regeneration, and human-only verification failures; verify with annotated tests

## 4. Configuration fixers (B3)

- [ ] 4.1 Add guided configuration fixes and human-only version and organization items; verify with annotated tests

## 5. Shared aggregates for every target (B4)

- [ ] 5.1 Drive shared-aggregate repair from every target that declares native aggregates; verify with annotated tests

## 6. JSON plan output (B5)

- [x] 6.1 Add `doctor --repair --json`; verify with annotated tests and a command-path witness

## 7. Journeys and archive (B7)

- [ ] 7.1 Add broken-to-healthy E2E journeys for interrupted writes, lock problems, aggregate collisions, and configuration errors; verify each ends with a clean `sync --check`
- [ ] 7.2 Reconcile documentation with the archived specs
- [ ] 7.3 Run `openspec archive doctor-repair-planner --yes`
- [ ] 7.4 Before final delivery to main and release, select a new major CLI release for the breaking adoption-prompt change and verify that its release notes include the upgrade guidance
