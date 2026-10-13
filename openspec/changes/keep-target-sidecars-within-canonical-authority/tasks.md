## Implementation

- [x] Add regressions through the Marketplace payload decoder and public `Compile`/`Convert`, annotated `// spec: harness-compilation/<slug>` for all six scenarios, and confirm the widening cases fail on the prior code.
- [x] Add the shared subagent tool-sidecar check and apply it in the Claude and Cursor subagent compilers.
- [x] Refuse contradictory Claude and Cursor hook-type sidecars in the ordinary hook compilers.
- [x] Apply the canonical Codex MCP match on the Codex target.
- [x] Reserve every modeled MCP connection field and its case variants in the shared MCP entry builder.
- [x] Update existing sidecar precedence tests, the field matrix and the changelog.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [ ] `openspec archive keep-target-sidecars-within-canonical-authority --yes`
