## 1. Catalog client and cache

- [x] 1.1 Add red tests for paginated catalog scans, identity/version/cursor failures, limits, ETag reuse and safe complete-cache round trips; verify focused tests fail on the base and pass with implementation.
- [x] 1.2 Implement the bounded `/api/v1/catalog` client, full per-endpoint scan, and safe complete-cache read/write; verify focused `internal/marketplace` tests pass.

## 2. Code-mode integration

- [x] 2.1 Add red snapshot tests for cold stale, refresh completion and reuse, available MCP refs, unavailable/rejected filtering and gate preservation; verify focused tests fail on the base and pass with implementation.
- [x] 2.2 Integrate cached startup and network refresh outcomes into the code-mode snapshot and stdio server; verify focused `internal/codemode` and `cmd/agent-marketplace` tests pass.
- [x] 2.3 Add red script-binding tests for lowercase listTools fields, object inputSchema, and JSON tool-call results, then implement plain JSON conversion; verify focused `internal/codemode` tests pass.
- [x] 2.4 Add a fresh-state stdio E2E test loading a Marketplace skill and declaring/calling a Marketplace MCP server after refresh, with every new scenario's `// spec:` annotation; verify `CGO_ENABLED=0 go -C e2e test -run '^TestCodeMode' -count=1 .` passes.

## 3. Delivery

- [x] 3.1 Reconcile code-mode bootstrap skill claims and run focused Go tests, `make spec-check`, `make validate-full`, and strict OpenSpec validation; verify each command succeeds.
- [ ] 3.2 Run `openspec archive code-mode-marketplace-catalog --yes` in this pull request and verify the main `code-mode-mcp` spec includes the new scenarios.
