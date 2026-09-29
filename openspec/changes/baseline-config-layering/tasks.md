## 1. Relocate the consumer Marketplace configuration scenarios

- [ ] 1.1 Move the three `// spec: marketplace-api/...` annotations in `internal/config/marketplace_test.go` to the new `config-layering/` identities from the design's slug map; comment lines only, no test-logic change
- [ ] 1.2 Add one `// spec:` annotation line to each existing passing test named in the design's slug map, in `internal/config/marketplace_test.go`, `internal/config/v4_test.go`, `internal/config/v4_migration_test.go`, and `internal/config/v3_test.go`; comment lines only, no new test logic; verify each bound test genuinely proves its scenario
- [ ] 1.3 Edit `openspec/specs/marketplace-api/spec.md` directly to drop the "consumer Marketplace configuration" phrase from the Purpose (the pinned CLI's sanctioned way to change an existing capability's Purpose)
- [ ] 1.4 Verify the focused tests stay green: `CGO_ENABLED=0 go test ./internal/config -run 'Marketplace' -count=1`

## 2. Absorb harvest rows 1.1 and 4.14

- [ ] 2.1 Check off rows 1.1 and 4.14 in `openspec/changes/harvest-legacy-docs/tasks.md`
- [ ] 2.2 Delete `docs/PRECEDENCE.md` and `docs/plans/2026-09-07-unified-resource-config-design.md`
- [ ] 2.3 Remove or repoint every remaining reference to the deleted documents in `README.md`, `AGENTS.md`, `docs/adoption-guide.md`, `docs/tui-product-contract.md`, and `docs/plans/2026-09-24-pre-v4-regression-gates.md`; verify no surviving file outside `openspec/changes/` links to either deleted document (the harvest ledger's checked rows and this change's own artifacts name the paths as backticked mentions, not links)
- [ ] 2.4 Verify every claim under the design's Discarded claims is genuinely not carried into the spec and every carried claim has an annotated test, by re-reading both deleted documents against the change

## 3. Verify and archive

- [ ] 3.1 Run `npx --yes @fission-ai/openspec@1.12.0 validate baseline-config-layering --strict --no-interactive` and `make spec-check`; both must pass
- [ ] 3.2 Run `openspec archive baseline-config-layering --yes` as the final commit of the pull request, then re-run `make spec-check` and `npx --yes @fission-ai/openspec@1.12.0 validate --all --strict --no-interactive`; if archive reports unaccounted wrapped lines in the removed requirement, follow design D6 and rerun
