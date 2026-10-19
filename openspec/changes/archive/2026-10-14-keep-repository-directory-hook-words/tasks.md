## 1. Tests

- [x] 1.1 Add compiler regressions for whole-word, path-component, and joined project-directory references on Windsurf (`pkg/harness-compilation/hook_cross_target_test.go`)
- [x] 1.2 Confirm they fail before the fix: whole-word forms emit an empty word and joined forms install a different path
- [x] 1.3 Bind the E2E handler execution test to the whole-word scenario (`e2e/hook_user_invariants_test.go`)

## 2. Implementation

- [x] 2.1 Emit `.` for a whole-word reference and refuse joined text in the `{cwd: repository}` rewrite (`pkg/harness-compilation/hookpolicy/resolve.go`)

## 3. Verification

- [x] 3.1 Run the compiler, materialization, and command packages, the E2E handler execution test, and `make spec-check`
- [x] 3.2 Run `make validate`, `make validate-e2e`, and the released-client gate
