---
name: code-mode
description: Use the agent-marketplace code-mode MCP server to search the resource catalog and run MCP tools through sandboxed code instead of loading every skill up front.
---

# Code mode

Code mode exposes two tools over the `agent-marketplace` MCP server:

- `search` runs a read-only async function against the merged registry and
  Marketplace catalog snapshot (`catalog`).
- `execute` runs an async function with declared `servers` bound as
  `mcp.<alias>` clients and `skills.load`/`skills.read` for on-demand skill
  content.

Declare exactly the catalog `mcp` refs a call needs in `servers`; undeclared
aliases fail closed. Results are bounded (deadline, output cap, host-call
cap) and failures carry stable coded errors.

Use `agent-marketplace promote <ref>` to turn a session-logged `skill.load`
or `server.start` into a durable installed selection.
