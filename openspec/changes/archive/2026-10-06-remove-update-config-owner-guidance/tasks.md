## 1. Regression tests

- [x] 1.1 Assert that default and verbose update output omit organization config owner guidance; bind both scenarios and confirm the regressions fail.

## 2. Implementation and validation

- [x] 2.1 Remove the instruction from update output and the CLI reference while preserving the pin summary and verbose listing.
- [x] 2.2 Run focused update tests, the output benchmark, validation, and spec checks; review the final diff.

## 3. Archive

- [x] 3.1 `openspec archive remove-update-config-owner-guidance --yes`

## Validation evidence

- Both output regressions failed on the owner instruction before implementation and passed after its removal.
- Focused update tests, documentation and command guards, and the two verbose update CLI E2E scenarios passed.
- `make validate` passed all root-module short-mode tests, lint, and build. `make spec-check` passed.
- The output benchmark measured 383.4 ns/op before and 374.8 ns/op after, with zero bytes and allocations per operation (five samples each).
- Adversarial review covered the PR delta against its merge base with `origin/main`, plus this removal on `ebc69a22`: command-to-output reachability, writer error propagation, unintended writes, organization scope boundaries, and regression validity. No actionable findings.
