## 1. Source field arrows

- [x] 1.1 Make `↑`/`↓` move Source-step focus between URL, Ref, and Path without wrapping, and update the footer hint; verify Go tests in `internal/tui/install_source_test.go` pass
- [x] 1.2 Extend `tools/tui-qa/tests/install-from-source.spec.ts` with a journey annotated `// spec: tui-interactions/arrow-keys-move-between-source-fields`; verify the journey passes
- [x] 1.3 Update `site/docs/reference/tui.md` and `docs/tui-product-contract.md`; verify they match the footer hint

## 2. Gates

- [x] 2.1 Run `make spec-check` and `go run ./tools/specaudit gate -base origin/main -head HEAD`; verify both pass
- [x] 2.2 Run `openspec archive tui-install-source-field-arrows --yes`
