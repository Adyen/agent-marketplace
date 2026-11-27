# Tasks

## 1. Git registry compatibility

- [x] 1.1 Separate registry URL normalization from strict profile identity; verify `TestCanonicalRepositorySuffix` and `TestRegistryRepeatedGitSuffix` pass.

## 2. Profile inspection publication

- [x] 2.1 Guard the current profile assessment pointer before file-managed upload; verify `TestProfileInspectionAddressIsImmutable` passes on SQLite and PostgreSQL.

## 3. Place initial-load timing

- [x] 3.1 Reuse the hosted collection fixture and verify the whole-invocation `TestPlaceInitialLoadWaitsForAllAdmittedRoots` without production timing changes.

## 4. Delivery gates

- [x] 4.1 Run `make spec-check`.
- [x] 4.2 Run `make validate TEST_PACKAGES='./cmd/agent-marketplace ./internal/gitidentity ./internal/lockfile ./internal/marketplace ./pkg/marketplace/store ./pkg/marketplace/store/sqlite ./pkg/marketplace/store/postgres'`.
- [x] 4.3 Run `make validate-e2e`.

## 5. Archive

- [x] 5.1 Run `openspec archive scope-place-review-corrections --yes` after final instruction.
