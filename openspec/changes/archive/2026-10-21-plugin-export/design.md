## Context

This change is the export slice of the draft `plugin-packages` change (draft
task 6.3, design D10). Plugin decoding, locks, inspection, and the
process-spawn guard already exist from earlier plugin changes.

## Goals / Non-Goals

**Goals:** a locked plugin can be exported as an Agent Plugins 1.0.0
package; every loss is listed; nothing is written unless the whole export
succeeds; no option drops a security restriction; export runs no package
process.

**Non-Goals:** native package export (`claude-plugin`, `factory-plugin`,
`codex-plugin`), native catalog export for authors, TUI or telemetry
surfaces, documentation, and any use of `convert`.

## Decisions

### D1: A pure exporter in harness compilation

The exporter takes a decoded `ir.Plugin` and its package files and returns
the files to write, the losses, and the restriction refusals. It performs no
I/O, so loss detection and output are computed before the CLI touches
`--out`.

### D2: Export reads the exact locked plugin

`export` resolves the plugin the way `inspect` does, at its exact locked
revision, and refuses a plugin that is not locked, not found, or available
from more than one registry.

### D3: Agent Plugins mapping

The package gets `plugin.json` with the canonical `$schema` and the manifest
fields Agent Plugins 1.0.0 declares, skills as immediate children of
`skills/`, and MCP servers in `mcp.json` using the stdio, streamable-http,
and sse variants. Hooks, commands, subagents, and any field or file the
format cannot hold are losses.

### D4: Security restrictions

A security restriction is an explicitly set MCP `disabled`, a skill
`permissions` policy, or skill triggers that forbid model invocation.
Dropping one fails the export even with `--allow-loss`. Omitting a whole
member is a loss, not a dropped restriction.

### D5: All-or-nothing output

`--out` must be absent or an empty directory, and an existing non-directory
is refused. The package is staged completely and published only after it
succeeds; a failure leaves `--out` absent or empty. Only `agent-plugin` is
accepted for `--format`; any other value is refused with
`plugin.format.unsupported` before writing.

## Risks / Trade-offs

- Agent Plugins readers ignore unknown skill frontmatter, so copying such
  fields would silently drop their meaning; the exporter counts them as
  losses instead.
