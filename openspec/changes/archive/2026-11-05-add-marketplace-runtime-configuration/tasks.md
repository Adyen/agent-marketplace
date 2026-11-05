# Tasks

## 1. Canonical generation and classification

- [x] 1.1 Add operator-config tests with independently authored expectations for the canonical indexing generation (formatting, comments, key order, and explicit defaults give the same generation; each projected field changes it; secret values and token contents do not) and for classification (added, removed, indexing changed, metadata-only, refresh interval, each restart-required field), bound to `marketplace-runtime-configuration/the-indexing-generation-excludes-secret-values` and the classification scenarios they prove; verify they fail on the base commit
- [x] 1.2 Extract parsing from bytes, implement the canonical generation and classification in the operator-config package, and replace the worker's startup generation with it; verify the tests from 1.1 and the existing worker and config tests pass

## 2. Store reconciliation and publication precondition

- [x] 2.1 Add conformance cases run against SQLite and against PostgreSQL when `POSTGRES_DSN` is set for atomic configuration changes with a failure on the last change and for publication with a stale expected generation, bound to the two `marketplace-registry-observation` scenarios this change adds; verify they fail on the base commit
- [x] 2.2 Implement atomic registry configuration application and the in-transaction expected-generation check in both backends; verify the cases from 2.1 pass on SQLite and on PostgreSQL with `POSTGRES_DSN` set, with no PostgreSQL skip

## 3. Runtime source and worker reconciliation

- [x] 3.1 Add runtime source tests for unchanged bytes, atomic rename replacement, partial, missing, and invalid candidates, restart-required reporting and clearing, and stopping on cancellation, bound to their `marketplace-runtime-configuration` scenarios; verify they fail on the base commit
- [x] 3.2 Add worker integration tests against local fixture repositories for formatting-only edits, added, same-commit indexing change, presentation-only change, removed registries, refresh interval change, the stale-generation interleaving, and the earlier generation scheme upgrade, bound to their scenarios; verify they fail on the base commit
- [x] 3.3 Implement the runtime source, wire it through `serve` and the worker, reconcile candidates, read the current snapshot in the observer and refresh processing, and add stale-generation handling; verify every scenario test and the existing `./pkg/marketplace/...` and `serve` command tests pass

## 4. Documentation

- [x] 4.1 Update the operator configuration reference and the reindex-and-monitor how-to to describe which configuration changes apply without a restart, the restart-required report, and the one-time refresh after upgrade; verify the described behavior matches the code

## 5. Integration

- [x] 5.1 Run `go run ./tools/specaudit trace`, `make spec-check`, `make test TEST_PACKAGES="./pkg/marketplace/..." TEST_FLAGS="-count=1"`, and `make validate`; verify all pass
- [x] 5.2 Run `openspec archive add-marketplace-runtime-configuration --yes` as the final commit and verify `make spec-check` still passes
