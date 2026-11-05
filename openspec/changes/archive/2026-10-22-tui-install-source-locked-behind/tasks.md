## 1. Lock-pinned refusal

- [x] 1.1 Record whether a prepared install kept a source's commit from the target layer's existing lock, and refuse a mismatching expected commit with `SourceLockedError` instead of `SourceMovedError`; verify a command test annotated `// spec: resource-installation/lock-pinned-source-refuses-an-expected-commit-install-with-update-remediation` passes
- [x] 1.2 Return the modal to the Review step with picks, scope, and remediation kept on `SourceLockedError`, without previewing again; verify Go tests in `internal/tui/install_source_test.go` pass
- [x] 1.3 Extend `tools/tui-qa/tests/install-from-source.spec.ts` with a journey annotated `// spec: tui-interactions/lock-pinned-source-returns-to-review-with-update-remediation`; verify it fails without the fix and passes with it

## 2. Gates

- [x] 2.1 Run `make spec-check` and `go run ./tools/specaudit gate -base origin/main -head HEAD`; verify both pass
- [x] 2.2 Run `openspec archive tui-install-source-locked-behind --yes`
