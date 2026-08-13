## Why

The `mcp/remove_roundtrip` E2E scenario fails after removing the only managed MCP server. Its assertion requires `_managed_by` to remain in `mcp.json`, contradicting the intended ownership model: the marker exists only on marketplace-managed entries and must disappear when the last such entry is removed.

## What Changes

- Correct the E2E expectation for removing the final managed MCP server.
- Require the retained MCP file to remain valid JSON with an empty `mcpServers` object.
- Preserve existing production merge and pruning behavior, which already removes only marketplace-owned entries.

## Capabilities

### Modified Capabilities

- `mcp-removal-roundtrip`: Defines the expected valid, marker-free configuration after the final managed MCP server is removed.

## Impact

- Affected code: `e2e/table_test.go` only.
- No runtime behavior, configuration schema, or dependencies change.
