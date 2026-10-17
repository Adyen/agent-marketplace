## Implementation

- [x] Add public regressions for the three scenarios and confirm they fail on the prior code.
- [x] Keep bare `$NAME` literal in Windsurf compilation and remove the Codex Windsurf-only refusal.
- [x] Canonicalize bare `$NAME` in Gemini MCP server string values on ingestion.
- [x] Refuse Gemini MCP servers whose emitted values contain a bare `$NAME`; add `mcp.gemini.interpolation-unrepresentable`.
- [x] Update affected tests, the field matrix and the changelog.
- [ ] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [ ] `openspec archive keep-bare-dollar-mcp-values-literal --yes`
