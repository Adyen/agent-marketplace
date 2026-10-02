## 1. Create missing roots

- [x] 1.1 Annotate `TestInstallUserScopeCreatesAMissingUserConfig` in `e2e/install_user_scope_new_root_test.go` and `TestCanonicalConfigPathTolerateMissingResolvesTheDeepestExistingAncestor` in `internal/config/loader_test.go` with `// spec: config-mutation/user-scope-install-creates-a-missing-user-root`; run both focused tests.
- [x] 1.2 Annotate `TestInstallUserScopeWithoutHarnessesUsesTheDefaultTargets` with `// spec: config-mutation/new-user-root-declares-the-default-harness-targets`; run the focused test.
- [x] 1.3 Annotate `TestInstallUserScopeMarketplaceCreatesAMissingUserConfig` and `TestMarketplaceLayerLockPathForAConfigWhoseDirectoryIsMissing` in `internal/pipeline/marketplace_layer_lock_path_test.go` with `// spec: config-mutation/marketplace-install-creates-a-missing-user-root-and-its-acquisition-lock`; run both focused tests.

## 2. Dangling symlinks

- [x] 2.1 Annotate `TestUserScopeRefusesToWriteThroughADanglingSymlink` and `TestCanonicalConfigPathTolerateMissingRefusesADanglingSymlink` with `// spec: config-mutation/user-scope-install-through-a-dangling-symlink-is-refused`; run both focused tests.
- [x] 2.2 Annotate `TestUserScopeRefusesToWriteThroughADanglingSymlink` with `// spec: config-mutation/dangling-user-config-reads-as-absent`; run the focused test.

## 3. Verify and archive

- [x] 3.1 Run `make spec-check`, `go run ./tools/specaudit gate -base origin/main -head HEAD`, `make validate`, and the full E2E suite; verify every scenario has a same-PR test annotation.
- [x] 3.2 Run `openspec archive create-missing-config-roots --yes` in this pull request after the annotated tests and validation pass; verify the archived spec contains the five scenarios.
