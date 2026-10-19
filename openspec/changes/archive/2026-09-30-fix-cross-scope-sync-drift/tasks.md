## 1. Failing regression coverage

- [x] 1.1 Add resolver tests for complementary user/repository selections and within-scope collisions, then confirm the cross-scope case fails on the original resolver.
- [x] 1.2 Add materialization tests for absolute machine-scope and relative repository-scope Factory hook commands, then confirm the machine case fails on the original materializer.
- [x] 1.3 Add a CLI test for both sync orders and a declarative E2E case; confirm both fail on the original behavior.

## 2. Behavior and specification

- [x] 2.1 Resolve cross-scope selections per scope without changing same-scope precedence or hook selection.
- [x] 2.2 Keep machine-scope hook paths absolute regardless of the sync location.
- [x] 2.3 Add scenario annotations in test files and the user-observable specification deltas.

## 3. Validation and delivery

- [x] 3.1 Run focused tests, review consumers and the diff, and run `make validate`.
- [x] 3.2 Commit the implementation locally without pushing or opening a pull request.
- [x] 3.3 `openspec archive fix-cross-scope-sync-drift --yes`
