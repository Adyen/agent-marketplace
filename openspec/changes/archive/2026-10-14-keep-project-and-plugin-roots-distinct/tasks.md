## Implementation

- [x] Add public `Convert` regressions annotated `// spec: harness-compilation/<slug>` for all three scenarios, and confirm they fail on the prior code.
- [x] Map only `${extensionPath}` to the canonical plugin root in Gemini ingestion.
- [x] Canonicalize `${DROID_PLUGIN_ROOT}` in Factory MCP ingestion and emit it from Factory MCP compilation.
- [x] Update affected unit tests, the field matrix and the changelog.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [x] `openspec archive keep-project-and-plugin-roots-distinct --yes`
