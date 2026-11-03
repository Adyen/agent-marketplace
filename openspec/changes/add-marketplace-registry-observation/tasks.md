# Tasks

## 1. Conformance suite first

- [x] 1.1 Add the table-driven observation conformance suite with literal expected values, run by one SQLite top-level test and one PostgreSQL top-level test gated on `POSTGRES_DSN`, with a `// spec: marketplace-registry-observation/<scenario-slug>` binding for every scenario in the delta spec; verify it fails on the base commit because the observation operations are missing
- [x] 1.2 Add migration upgrade and repeat tests for both backends that open a database at the preceding schema version with existing registries, occurrences, heads, and jobs; verify they fail on the base commit

## 2. Domain and migrations

- [x] 2.1 Add the registry observation, registry lease, refresh request/decision, and host budget domain types and the new `MetadataStore` operations; verify `go build ./...` succeeds
- [x] 2.2 Append one migration per backend creating the observation and host budget tables; verify the upgrade and repeat tests pass on SQLite and PostgreSQL

## 3. Store operations

- [x] 3.1 Implement observation create, get, compare-and-swap update, delete, and due selection on both backends; verify the observation, due-selection, compare-and-swap, and removal cases pass
- [x] 3.2 Implement the registry work lease (acquire, renew, release) on both backends; verify the lease cases pass
- [x] 3.3 Implement host budget admission and release on both backends; verify the host budget cases pass
- [x] 3.4 Implement atomic refresh requests with coalescing, and clear the active refresh inside terminal `FailJob`; verify the refresh cases pass

## 4. Integration

- [x] 4.1 Run `make spec-check`, `make test TEST_PACKAGES="./pkg/marketplace/..." TEST_FLAGS="-count=1"`, and the witnessed PostgreSQL conformance run with `POSTGRES_DSN` set; verify all pass with no PostgreSQL skip
- [ ] 4.2 Run `openspec archive add-marketplace-registry-observation --yes` as the final commit and verify `make spec-check` still passes
