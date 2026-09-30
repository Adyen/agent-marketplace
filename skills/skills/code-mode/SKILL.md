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
aliases fail closed. Refs are `<registry>/<kind>/<name>`: directory sources
use `local:<source name or dir>`, named Git sources use `<source name>`, and
Marketplace resources use `marketplace:<registry slug>`.

Results are bounded (deadline, output cap, host-call cap). An oversized
result is replaced by `{"truncated": true, "bytes": <serialized size>,
"prefix": <leading excerpt>}` and flagged `truncated`. Failures thrown into
script code arrive as `Error` instances named `CodeModeError` with lowercase
`code` and `message`; the same coded error reaches the tool boundary.

Server definitions may reference the host environment with `${VAR}` or
`${env:VAR}` in `command`, `args`, `env` values, `url`, and `headers`;
references are expanded before the server starts, and unresolved or
non-environment references fail `mcp.start-failed`.

Use `agent-marketplace promote <ref>` to turn a session-logged `skill.load`
or `server.start` into a durable installed selection.
