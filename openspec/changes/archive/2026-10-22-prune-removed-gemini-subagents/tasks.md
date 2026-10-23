## 1. Implementation

- [x] 1.1 Add regression tests that fail without the fix: a materialize test (`// spec: materialization/deselected-gemini-subagents-are-removed`), command tests for `remove` with `sync` and for `deinit`, e2e table cases for repository and user scope, a backup coverage test, and a layout test that pins the compiler's subagent directory
- [x] 1.2 Declare `agents` in the Gemini native layout components and update the component expectation

## 2. Archive

- [x] 2.1 Run `openspec archive prune-removed-gemini-subagents --yes`
