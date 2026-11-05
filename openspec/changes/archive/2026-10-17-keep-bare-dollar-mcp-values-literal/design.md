## Context

Documented expansion (checked 2026-10-16):
- Claude Code MCP values: `${VAR}` and `${VAR:-default}`; a bare `$VAR` is literal.
- Windsurf `mcp_config.json`: `${env:VAR}` and `${file:PATH}`.
- Gemini CLI `settings.json` and `gemini-extension.json`: `$VAR`, `${VAR}` and `${VAR:-default}` in any string value; no escape is documented.
- Codex MCP values are literal (#249).

## Decisions

1. **Canonical meaning is Claude's.** Only braced forms are references; Windsurf compilation stops treating a bare `$NAME` as one.
2. **Gemini ingestion braces its bare references.** Every `$NAME` (`$` followed by a word character, Gemini's own pattern) in a settings or extension MCP server string value becomes `${NAME}`, so other targets keep the reference.
3. **Gemini compilation refuses what it would reinterpret.** A server whose emitted string values (canonical fields and extension fields) contain a bare `$NAME` is suppressed with `mcp.gemini.interpolation-unrepresentable`, the same entry-level outcome as `mcp.devin.interpolation-unrepresentable`.
4. **The #249 Windsurf branch goes away.** Codex literals were refused on Windsurf only because of the rewrite; with the rewrite gone they convert intact. The `${` guard for Codex literals stays.

## Non-goals

Gemini subagent inline MCP servers, whose expansion behaviour is undocumented; command prompts and resource bodies.

## Verification

Public `Convert` and `Compile` regressions fail before the change: Windsurf rewrote `$NOT_A_REF`, refused Codex literals, Gemini references stayed literal, and Gemini accepted canonical literals.
