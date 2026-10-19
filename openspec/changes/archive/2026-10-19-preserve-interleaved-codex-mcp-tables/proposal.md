## Why

An owned Codex MCP server in `config.toml` can span several TOML tables, for example `[mcp_servers."foo"]` and `[mcp_servers."foo".env]`. Agent Marketplace located each server as one byte range from its first table to its last. When a handwritten table sat between those tables, updating or retiring the server rewrote or removed the whole range and deleted the handwritten table, while sync reported success. A handwritten array table (`[[...]]`) after a server's table was also absorbed into the server's range. Doctor repair and owned-entry cleanup used the same range and had the same loss.

## What Changes

- Each owned Codex MCP server is located as the set of its own tables. Consecutive tables of one server still form one range, so files without interleaving are edited byte-for-byte as before.
- Updating an owned server rewrites its first range and removes its other ranges; pruning, repair removal and owned-entry cleanup remove only its ranges. Interleaved handwritten tables and array tables are preserved byte-for-byte.
- Several servers added in one run are appended in their configured order instead of in reverse.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `owned-entry-reconciliation`: editing an owned entry preserves unmanaged content interleaved with the entry's native representation.

## Impact

Codex MCP aggregate reconciliation, repair and cleanup. No configuration, lock, ownership-state or diagnostic format changes; owned servers using alternate TOML forms keep their existing refusal.
