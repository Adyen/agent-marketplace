## 1. Workspace behavior

- [x] 1.1 Keep repository and personal-local roots aligned through resolution and sync; verify `TestSyncNestedLocalLoadsRepoConfigAndMaterializesBelowIt` passes.
- [x] 1.2 Reject escaping or conflicting roots before mutation; verify `TestSyncFailsBeforeLockPreparationWhenWorkspaceDiscoveryFails` and `TestNewWorkspaceRejectsMissingLeafBeneathEscapingSymlink` pass.

## 2. Contract and verification

- [x] 2.1 Annotate the existing nested-sync and escaping-root tests for both workspace-resolution scenarios; verify `go run ./tools/specaudit trace` passes.
- [x] 2.2 Run `openspec validate validated-workspace-resolution --strict` and the focused workspace tests.
- [x] 2.3 Run `openspec archive validated-workspace-resolution --yes` as the last change in this pull request; verify the archived spec and bindings remain valid.
