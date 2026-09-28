## 1. Rename traceability

- [x] 1.1 Add annotated trace cases for rename plus modification, rename plus removal, and rename-only preservation; observe the modification and removal cases fail before the fix.
- [x] 1.2 Parse rename mappings and apply them before other delta sections; verify `CGO_ENABLED=0 go test ./tools/specaudit -count=1` passes.
- [x] 1.3 Verify `make spec-check`, `go vet ./tools/specaudit`, and the original external rename fixture.
- [x] 1.4 Run `openspec archive fix-specaudit-renames --yes` and verify the archived contract with `make spec-check`.
