## Implementation

- [x] Add failing regressions for update and retirement with an interleaved handwritten table and array table, and for repair removal and replacement; annotate each scenario.
- [x] Locate each Codex MCP server as its own table ranges, attribute array tables, and edit only those ranges in sync, repair and cleanup.
- [x] Prove span splitting and array-table attribution with negative mutations and rerun the frozen interleaved-table reproduction.
- [x] Verify scoped materialization tests, lint, build and strict specs.
- [x] Adversarially review every changed entry.
- [x] `openspec archive preserve-interleaved-codex-mcp-tables --yes`
