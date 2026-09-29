## 1. Backfill code-mode-mcp specification

- [x] 1.1 Derive requirements and scenarios from the implemented opt-in
  config/registration, two-tool MCP surface, sandboxed execution, security
  boundaries, session log, and promote flow.
- [x] 1.2 Annotate the existing tests proving each scenario with
  `// spec: code-mode-mcp/<scenario-slug>` lines.
- [x] 1.3 Verify `go run ./tools/specaudit trace`, `go run ./tools/specaudit
  archived`, and `openspec validate --all --strict` pass; focused code-mode
  Go tests remain green.
- [x] 1.4 Archive the change with `openspec archive code-mode-mcp --yes`.
