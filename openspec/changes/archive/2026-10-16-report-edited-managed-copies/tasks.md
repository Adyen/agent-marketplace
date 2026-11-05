## 1. Tests

- [x] 1.1 Add failing tests annotated `// spec: materialization/<slug>` for each scenario in the delta spec (skill tree, single-file artifact, return to symlinks, unrecorded output unchanged, delete-and-sync restore) and verify they fail on the base for the intended reason

## 2. Implementation

- [x] 2.1 Report recorded-and-changed file-mode copies with the locally edited managed copy message in the skill-tree, single-file, and return-to-symlinks paths through one shared helper, and verify the new tests and existing `internal/materialize` collision tests pass
- [x] 2.2 Run `go run ./tools/specaudit trace` and verify every new scenario is bound

## 3. Archive

- [x] 3.1 Run `openspec archive report-edited-managed-copies --yes` and verify `openspec validate --all --strict` passes
