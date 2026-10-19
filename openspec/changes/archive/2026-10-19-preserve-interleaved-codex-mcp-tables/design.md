## Context

Codex MCP reconciliation parses `config.toml` with the streaming TOML parser and records, per server, the byte range to rewrite or delete. Sync, doctor repair and owned-entry cleanup all edit through that range.

## Decisions

- Record a list of ranges per server. A table or array table of a server extends the server's last range only when the immediately preceding table header belongs to the same server; otherwise it starts a new range. Files without interleaving keep a single range identical to the previous one, so their edits are unchanged.
- Treat array-table headers as table boundaries. A foreign `[[...]]` table ends the current server, and a server's own array tables join its ranges.
- Replacement writes the configured fragment at the first range and deletes the others; pruning and removal delete every range. All edits are applied from the end of the file backwards.
- Alternate TOML forms (inline keys under `[mcp_servers]` and top-level dotted keys) keep a single range and their existing refusal.

## Alternatives Considered

- Refusing servers split by unmanaged content with `materialize.codex.aggregate-unpatchable`: rejected because a drifted owned entry must be restored and a stale one pruned, and a refusal would leave a retired MCP server installed.
