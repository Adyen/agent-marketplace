## 1. Shared test-only foundation

- [x] 1.1 Add `pgregory.net/rapid` v1.3.0 to `go.mod`/`go.sum`; verify the production `go list -deps` graph is unchanged apart from test files
- [x] 1.2 Add `cmd/agent-marketplace/rapid_guard_test.go` (`TestRapidProductionDependencyGuard`) with the cobra non-vacuity control and register it in `command_integration_test.go`; verify it passes
- [x] 1.3 Ignore `testdata/rapid/` in `.gitignore` and record the property-test standards in `CODING_STANDARDS.md`

## 2. Archive

- [x] 2.1 Run `openspec archive rapid-property-testing-infra --yes` as the final step of this pull request
