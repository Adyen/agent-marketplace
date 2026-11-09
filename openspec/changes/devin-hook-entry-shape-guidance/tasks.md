# Tasks

## 1. Entry-level guidance

- [ ] 1.1 Add command tests for repository and user scope Devin hook entry refusals asserting the entry-specific `hint:` line and an unchanged file (bytes, mode, modification time), plus a root or member refusal asserting the catalog hint, and verify they fail before the fix
- [ ] 1.2 Give the entry-level `materialize.devin.aggregate-shape` refusal its entry-specific hint without changing code, message, or catalog, update the existing owned-aggregate guidance test to expect it, and verify the focused tests pass

## 2. Archive

- [ ] 2.1 Run `openspec archive devin-hook-entry-shape-guidance --yes` and verify `make spec-check` passes
