## 1. Health classification

- [x] 1.1 Keep warning-only inspection successful; verify `TestInspectionWarningOnlyRemainsSuccessful` passes.
- [x] 1.2 Report dry-run materialization failures through sync check; verify `TestSyncCheckSurfacesDryRunFailureAsInspectionError` passes.

## 2. Bind and archive

- [x] 2.1 Annotate the tests proving both inspection-outcomes scenarios; verify `go run ./tools/specaudit trace` passes.
- [x] 2.2 Run strict OpenSpec validation and focused inspection tests; verify both commands pass.
- [x] 2.3 Run `openspec archive consistent-inspection-outcomes --yes` as the final change in this pull request; verify the archived spec is present.
