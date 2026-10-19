## Implementation

- [x] Add a failing repair-preview regression for a duplicate desired Codex MCP key with an unrelated conflict, and annotate both scenarios.
- [x] Refuse duplicate desired keys in the Codex repair preview with `materialize.codex.duplicate-identity`, keep them desired, and label candidates with the resource whose value is written.
- [x] Name the resource and generated value digest in the replacement confirmation, and update the command tests that pin it.
- [x] Verify scoped materialize and doctor tests, lint, build and strict specs.
- [x] Adversarially review every changed entry.
- [x] `openspec archive refuse-duplicate-codex-repair-identities --yes`
