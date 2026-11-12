## 1. Tests first

- [ ] 1.1 Add cache tests for root-scoped acquisition, verification inside and outside the root, the non-tree fallback, the read-only whole-repository fallback, and GC reachability of root keys; verify they fail on the base commit
- [ ] 1.2 Add command tests annotated `// spec: exact-snapshot-verification/<slug>` for every ADDED scenario; verify they fail on the base commit

## 2. Implementation

- [ ] 2.1 Key, publish, and verify exact snapshots per configured root in `internal/cache`, and verify that the cache tests pass
- [ ] 2.2 Open exact snapshots per configured root in every command and pipeline caller, and verify that the command tests pass
- [ ] 2.3 Run `make test TEST_PACKAGES='./internal/cache ./internal/sourcerevision ./internal/pipeline ./cmd/agent-marketplace' TEST_FLAGS='-count=1'` and `make spec-check`, and verify that both pass

## 3. Archive

- [ ] 3.1 Run `openspec archive exact-git-root-snapshots --yes`
