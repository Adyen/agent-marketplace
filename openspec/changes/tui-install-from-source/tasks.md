## 1. Pipeline

- [ ] 1.1 Extract the `list --from` body into a shared pipeline source preview (resolved URL, ref, path, commit, catalog, bundle paths, warnings) and switch `list --from` to it; verify existing `list --from` tests pass unchanged and new preview tests cover allowlist refusal before fetch, default branch, bundle paths, and malformed resources
- [ ] 1.2 Expose the exact commit resolved by dependency preparation and add expected-commit checking to explicit-source installs; verify tests annotated `// spec: resource-installation/moved-source-refuses-an-expected-commit-install`, `matching-expected-commit-installs-at-the-requested-ref`, and `expected-commit-for-a-directory-source-is-refused` pass
- [ ] 1.3 Add kind-qualified locators to explicit-source installs; verify the test annotated `// spec: resource-installation/kind-qualified-picks-select-resources-that-share-a-name` passes

## 2. TUI

- [ ] 2.1 Add the `i` binding to the key map, help overlay, and footer, and refuse it while mutations are unavailable; verify a Go test and the journey annotated `// spec: tui-interactions/install-from-source-is-refused-while-mutations-are-unavailable`
- [ ] 2.2 Implement the Source, Pick, and Review steps with asynchronous preview, stale-response dropping, badges, filter, bulk toggle, diagnostics, bundle paths, scope eligibility, and compact layout; verify Go tests for step transitions and scope eligibility pass
- [ ] 2.3 Wire confirm through the guarded mutation path with the expected commit and locators, returning to Pick on a moved ref; verify Go tests pass

## 3. TUI QA and docs

- [ ] 3.1 Add the `adHocSource` fixture option and `tools/tui-qa/tests/install-from-source.spec.ts` covering every `tui-interactions` scenario of this change with `// spec:` annotations, register it in `tools/tui-qa/README.md`; verify `make tui-qa-check` and `make tui-qa` pass
- [ ] 3.2 Document the `i` binding and flow in `site/docs/reference/tui.md` and `docs/tui-product-contract.md`; verify the docs match the help overlay
- [ ] 3.3 Run `make spec-check`, `make validate`, and `go run ./tools/specaudit gate -base origin/main -head HEAD`; verify all pass
- [ ] 3.4 Run `openspec archive tui-install-from-source --yes`
