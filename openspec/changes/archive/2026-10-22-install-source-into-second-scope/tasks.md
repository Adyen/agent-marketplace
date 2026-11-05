## 1. Second-scope install

- [x] 1.1 Select the explicit-source catalog from the target scope's declaration; verify a command test annotated `// spec: resource-installation/source-locked-in-another-scope-installs-into-the-target-scope` passes and fails without the fix
- [x] 1.2 Restore "or choose another scope" in the lock-pinned remediation; verify `internal/tui` tests pass
- [x] 1.3 Extend the lock-pinned journey in `tools/tui-qa/tests/install-from-source.spec.ts` with the Local install and sync, annotated `// spec: tui-interactions/lock-pinned-source-installs-into-another-scope`; verify it fails without the fix and passes with it

## 2. Gates

- [x] 2.1 Run `make spec-check` and `go run ./tools/specaudit gate -base origin/main -head HEAD`; verify both pass
- [x] 2.2 Run `openspec archive install-source-into-second-scope --yes`
