## 1. Failing regression coverage

- [x] 1.1 Add resolver tests for collection members across scopes, required suppression through collections, and order-independent required suppression; confirm they fail against the original resolver.
- [x] 1.2 Add a command test that syncs a user collection and a repository skill in both orders and checks `sync --check` from both locations; confirm it fails against the original resolver.
- [x] 1.3 Add declarative E2E cases for the cross-scope collection member and the required collection member; confirm both fail against the original resolver.

## 2. Behavior and specification

- [x] 2.1 Admit collection members per install scope.
- [x] 2.2 Remove required shadows before the repository-scope winner is chosen, including required resources revealed by collection expansion, and remove remaining collection-member shadows.
- [x] 2.3 Annotate every new scenario in test files.

## 3. Validation and delivery

- [ ] 3.1 Run focused tests, benchmarks, `make validate`, `make spec-check`, `make validate-e2e`, and `make tui-qa`.
- [ ] 3.2 Run an adversarial review of the diff and commit the implementation.
- [ ] 3.3 `openspec archive fix-cross-scope-collection-members --yes`
