## 1. Preview cache reuse

- [ ] 1.1 Return the advertised default-branch commit from the same `ls-remote --symref` call that finds the default branch, and add a check that a cached checkout is at a given commit; verify cache unit tests pass
- [ ] 1.2 Make the source preview skip the fetch for a current default-branch cache and for a cached commit-SHA ref; verify tests annotated `// spec: resource-installation/default-branch-preview-skips-the-fetch-when-the-cache-is-current`, `default-branch-preview-refreshes-a-stale-cache`, and `commit-pinned-preview-works-from-the-cache-offline` pass and existing `list --from` tests pass unchanged

## 2. TUI navigation

- [ ] 2.1 Add `←`/`→` step navigation, preview reuse for an unchanged source, and `↑`/`↓`/`k`/`j` Review scope selection, with footer hints; verify Go tests in `internal/tui/install_source_test.go` pass
- [ ] 2.2 Extend `tools/tui-qa/tests/install-from-source.spec.ts` with every `tui-interactions` scenario of this change, annotated with `// spec:`; verify `make tui-qa` passes
- [ ] 2.3 Update `site/docs/reference/tui.md` and `docs/tui-product-contract.md`; verify they match the footer hints

## 3. Gates

- [ ] 3.1 Run `make spec-check`, `make validate`, and `go run ./tools/specaudit gate -base origin/main -head HEAD`; verify all pass
- [ ] 3.2 Run `openspec archive tui-install-source-navigation --yes`
