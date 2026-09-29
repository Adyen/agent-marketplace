## Why

Code mode only reads the first cached discovery page, and its background refresh makes no Marketplace network request. Marketplace MCP servers beyond that page cannot be found or used. Scripts also receive Go struct fields from `listTools()` instead of the documented JSON fields.

## What Changes

- Fetch all cursor pages of each configured Marketplace endpoint's catalog in the background, and persist only complete verified results for later sessions.
- Mark missing or incomplete catalog snapshots stale, without waiting for the initial refresh on cold start.
- Expose available Marketplace skills and MCP servers from the complete catalog through search and the existing verified execution gate.
- Return JSON-shaped MCP tool metadata and tool results to code-mode scripts.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `code-mode-mcp`: Complete Marketplace search snapshots, stale-cache behavior, dynamic Marketplace execution, and script-visible tool result fields.

## Impact

`internal/marketplace` catalog client and cache; `internal/codemode` snapshot and script bindings; `cmd/agent-marketplace/mcp.go`; focused and stdio E2E tests; the code-mode bootstrap skill if needed. The TUI's independent discovery path and MCP server startup policy do not change.
