## Why

Target sidecars carry harness-specific values next to the canonical resource. Several compilers let a sidecar replace an authority-bearing canonical value without checking it, so a Marketplace resource artifact whose canonical payload looks restricted can install with broader authority:

- A Claude or Cursor subagent tool sidecar replaces the canonical tool list, so a `Read`-only subagent can compile with `Bash`.
- A Claude or Cursor hook-type sidecar turns a canonical prompt hook into a command hook that runs its dormant command.
- A Codex MCP sidecar replaces the canonical command, arguments and environment forwarding.
- Canonical-shaped MCP keys such as `command` or `args` in extension fields become active server settings when the typed field is empty.

## What Changes

- A Claude or Cursor subagent tool sidecar may only restate or narrow the canonical tool list. A sidecar entry that is neither a canonical entry nor that entry's target mapping suppresses the subagent with `subagent.security-restriction-unsupported`.
- A Claude or Cursor hook-type sidecar must equal the canonical hook type; a contradiction suppresses the hook with `hook.claude.unsupported-type` or `hook.cursor.unsupported-type`, as native hook projection already does.
- Codex compilation emits a native MCP sidecar only when it matches the canonical server, the same check already applied when a Codex server leaves Codex; otherwise it suppresses the server with `mcp.codex.security-unsupported`.
- Claude, Cursor, Factory and Gemini MCP compilation reserve every modeled MCP connection field (`command`, `args`, `env`, `url`, `transport`, `headers` and the OAuth fields), including case variants, so extension fields cannot supply them; they are dropped with `compile.canonical-field-overrides-extra`. An extension `disabled` field can only disable a server and keeps its existing handling.

## Capabilities

### Modified Capabilities

- `harness-compilation`: target sidecars and extension fields cannot widen canonical authority.

## Impact

Claude, Cursor and Codex compilers and the shared MCP entry builder in `pkg/harness-compilation`. No in-tree ingester produces Claude or Cursor sidecars, and Codex-ingested sidecars already match their canonical server, so native round trips are unchanged. A library caller or Marketplace payload whose sidecar contradicts its canonical resource now has that resource suppressed with an error. No fixture executes a resource.
