## 1. Prove each scenario with an annotated test

- [x] 1.1 Annotate the PostgreSQL `TestMigrationsAreRerunnable` index
  assertions in `pkg/marketplace/store/postgres/store_test.go` with
  `// spec: digest-owner-lookup/postgresql-store-keeps-digest-owner-indexes`;
  verify `go test ./pkg/marketplace/store/postgres/ -run TestMigrationsAreRerunnable -count=1`
  passes when `POSTGRES_DSN` is set and skips cleanly otherwise
- [x] 1.2 Annotate the SQLite `TestMigrationsAreRerunnable` index assertions
  in `pkg/marketplace/store/sqlite/store_test.go` with
  `// spec: digest-owner-lookup/sqlite-store-keeps-digest-owner-index` and
  `// spec: digest-owner-lookup/pre-existing-sqlite-revision-index-is-preserved`;
  verify `go test ./pkg/marketplace/store/sqlite/ -run TestMigrationsAreRerunnable -count=1`
  passes
- [x] 1.3 Verify `go run ./tools/specaudit trace` reports no unbound
  `digest-owner-lookup` scenario and no dangling annotation

## 2. Validate and verify

- [x] 2.1 Run `openspec validate index-digest-owner-lookups --strict`,
  `go test ./pkg/marketplace/store/... -count=1`, `make ci-lint`, and
  `make build`; record environment-dependent skips
- [x] 2.2 Run `openspec archive index-digest-owner-lookups --yes` in this
  pull request; verify `openspec validate --all --strict` and
  `go run ./tools/specaudit trace` pass afterward
