## Context

MCP JSON merge artifacts mark marketplace-owned server entries with `_managed_by: "agent-marketplace"`. On removal, `stripManagedEntriesFromFile` removes those entries while deliberately preserving empty top-level containers. This retains a valid `mcp.json` shape:

```json
{
  "mcpServers": {}
}
```

The existing E2E assertion incorrectly expects an ownership marker after removing the only managed server. That marker would prove stale marketplace state, not successful cleanup.

## Decision

Update the declarative E2E assertion rather than production materialization.

- Assert that `mcp.json` still exists.
- Assert that it contains an empty `mcpServers` object.
- Assert that it contains neither `roundtrip-mcp` nor `_managed_by`.

## Risks

- An assertion based only on string absence could allow an invalid or deleted file. Require file existence and the empty container as positive checks.
- Do not alter merge or pruning behavior, which is already covered by unit tests and preserves user-owned MCP servers.
