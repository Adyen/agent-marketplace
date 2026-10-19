## Decision

Use the existing normal resolver and scope-aware removal lookup for organization
Marketplace resource opt-outs. Map the chosen artifact back to its verified
organization pin using the catalog key before reusing the exact exclusion edit.
Endpoint-qualified removal already identifies the source and keeps its current
path. Collection and non-organization Marketplace early removal stay available.

## Alternatives

- Enumerate `all`, tags, defaults, and collection membership in the early lookup:
  duplicates the resolver and risks missing another catalog-dependent selector.
- Refuse whenever a repository has a dynamic selector: safe but rejects valid
  removals even when the resource is not selected there.

## Verification

Start with an `all` collision regression that fails on the current branch, then
cover tags, repository collection members, direct organization selections,
explicit scopes, and endpoint qualification. Check both positive opt-out
effects and preservation after refusal. Keep undiscoverable-selection and
native-hook ownership regressions passing.
