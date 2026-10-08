## 1. Native boundary and regression witnesses

- [ ] 1.1 Add failing annotated public compile/materialize tests for all seven targets, both scopes, stdio/remote resources, malformed/case-variant namespace extras, unchanged input maps, and unrelated native field controls; record intended native-leak failures before implementation.
- [ ] 1.2 Add the shared MCP-only extra-key selector, route JSON MCP serializers through it, and exempt publishing-only extras from the Codex omission warning; verify the 1.1 witnesses pass without changing non-MCP extension handling.
- [ ] 1.3 Add annotated canonical restoration and historical identity witnesses; verify namespace-bearing payload/digest equality, metadata-free frozen canonical bytes, all historical source-revision vectors, and unchanged writer defaults.
- [ ] 1.4 Add an annotated actual synchronization witness with an owned server and handwritten sibling; verify publishing-only update, repeat/locked/check no-op bytes and mtimes, unchanged healthy lock, and no ownership expansion.

## 2. Delivery and documentation

- [ ] 2.1 Reconcile existing native-boundary documentation without advertising full publishing support; verify claims against the implemented consumer paths, focused tests, make validate, make spec-check, strict all-spec validation, and combined make validate-e2e.
- [ ] 2.2 Run `openspec archive mcp-publishing-metadata-native-fence --yes` in the implementation pull request; verify unchanged approved delta content, synchronized main specification, and passing make spec-check.
