## 1. Failing regression coverage

- [x] 1.1 Add resolver tests for a required collection member against a non-required direct selection in the same scope; confirm they fail against the original resolver.
- [x] 1.2 Add a command test that syncs in both orders and checks `sync --check` from both locations; confirm it fails against the original resolver.
- [x] 1.3 Add a declarative E2E case for the required collection member displacing a user selection.

## 2. Behavior and specification

- [x] 2.1 Let a required collection member displace non-required selections in its install scope.
- [x] 2.2 Report the displacement in one name-collision warning.
- [x] 2.3 Annotate every new scenario in test files.

## 3. Validation and delivery

- [x] 3.1 Run focused tests, `make validate`, `make spec-check`, `make validate-e2e`.
- [x] 3.2 Run an adversarial review of the diff and commit the implementation.
- [x] 3.3 `openspec archive fix-same-scope-required-collection-member --yes`
