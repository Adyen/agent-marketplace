## Why

Claude Code expands only `${NAME}` in MCP values, so a canonical bare `$NAME` is literal text. Windsurf compilation rewrote a whole-value bare `$NAME` to `${env:NAME}`, turning a literal into an environment lookup; #249 refused native Codex servers in that case but kept the rewrite for other sources (#638 D2). Gemini is the reverse case: it expands bare `$NAME` in settings and extension MCP values, so a Gemini reference was carried as a literal, and a canonical literal sent to Gemini became a lookup.

## What Changes

- Windsurf emits a bare `$NAME` unchanged. The #249 Codex-specific Windsurf refusal is no longer needed and is removed; Codex literals now convert to Windsurf intact.
- Gemini ingestion canonicalizes bare `$NAME` in MCP server string values to `${NAME}`.
- Gemini compilation refuses an MCP server whose emitted values contain a bare `$NAME`, with the new error `mcp.gemini.interpolation-unrepresentable` (maintainer decision on #638, part 3).

## Capabilities

### Modified Capabilities

- `harness-compilation`: bare dollar names in MCP values stay literal.

## Impact

Windsurf and Gemini MCP compilation, Gemini MCP ingestion, the Codex native-literal guard, one new error code, the field matrix and changelog. Gemini subagent inline MCP servers are unchanged.
