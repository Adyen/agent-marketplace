## 1. Deployed evidence

- [x] 1.1 Keep an absent organization lock distinct from deployed evidence; verify `TestReadDeployedLocksAbsentOrganizationIsNil` passes.
- [x] 1.2 Reject stale or malformed organization and missing writable deployed locks; verify `TestReadDeployedLocksStaleOrganizationGitLockFails`, `TestReadDeployedLocksMalformedOrganizationFails`, and `TestReadDeployedLocksMissingWritableGitLockFails` pass.

## 2. Bind and archive

- [x] 2.1 Annotate all nine acquisition-lock scenario tests; verify `go run ./tools/specaudit trace` passes.
- [x] 2.2 Run strict OpenSpec validation and focused acquisition tests; verify both commands pass.
- [x] 2.3 Run `openspec archive deployed-lock-acquisition --yes` as the final change in this pull request; verify the archived spec is present.
