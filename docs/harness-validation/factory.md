# Factory Droid validation

[Shared validation contract](README.md)

## Installed resources

The fixture uses the scope's `.factory` directory: skills under `skills/`,
commands under `commands/`, droids under `droids/`, and MCP entries in
`mcp.json`. The suite tests both repository and user scopes in both symlink
and file modes. Native inspection reads this output directly; it does not
register another plugin or force a different materialization mode.

## Skills, commands, and droids: native resource RPCs

The driver launches the pinned executable as:

```text
droid exec --input-format stream-jsonrpc --output-format stream-jsonrpc
```

It sends `droid.initialize_session` with the disposable workspace and a
fixture machine identifier. This starts a metadata session without sending a
user message. Factory's envelope includes `type: "request"`,
`factoryApiVersion`, `factoryProtocolVersion`, and a string request ID; bare
JSON-RPC requests with numeric IDs are not equivalent.

After a successful initialization, the driver calls one resource method:

| Resource | RPC | Assertions |
|---|---|---|
| Skill | `droid.list_skills` | Exact name/description, enabled state, project/personal location, installed or symlink-resolved file path |
| Command | `droid.list_commands` | Exact fixture name and description |
| Subagent/droid | `droid.get_context_breakdown` | Exact fixture name in the `droids` inventory and expected location; token estimates are not treated as execution evidence |

Each inventory must omit the fixture before installation and contain it
exactly once afterward. A process error, RPC rejection, missing inventory, or
mismatched field fails the resource probe. The driver sends no user-message
RPC, model request, permission bypass, or inline replacement droid.

Factory returns the bodies of its built-in skills with the skill inventory, so
the driver bounds this protocol response separately at 10 MiB, in line with
the SDK transport's documented support for multi-megabyte responses. Stderr
keeps the normal diagnostic limit. Tests cover both valid larger content and
an over-limit response. Skill bodies are not copied into compatibility
reports.

Implementation: [protocol transport](../../e2e/harness_compatibility_protocol_test.go)
and [inventory validation](../../e2e/harness_compatibility_inventory_test.go).

## MCP servers

`droid mcp list` must omit both fixture names before installation and show
them after `sync`. This is evidence that Factory recognizes the
configuration; the transport stays `not-probed`, and nothing here claims that
a model invoked either server.

## Remaining limits

- Hooks: `/hooks` is an interactive session manager, and the documented
  resource RPC surface has no installed-hook inventory.
- Rules: these CLI and RPC methods expose no named rule-file inventory. Tool
  lists and context-token totals are not evidence of rule discovery.
- Collections: virtual selections; validate their expanded members instead.

Keep fixture identities stable across materialization modes. Do not accept
truncated names, and do not replace a failing native probe with a file-only
check.

The [Factory hook event reference](https://docs.factory.ai/harness/hooks)
documents `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Notification`,
`Stop`, `SubagentStop`, `PreCompact`, `SessionStart`, and `SessionEnd`, and
conversion keeps these events. Factory does not document a separate
`PermissionRequest` event: permission prompts are notifications, and
permission decisions belong to `PreToolUse` output. Conversion must not
substitute either of those for a `PermissionRequest` hook. Unknown events are
suppressed with `hook.event-unmappable`.

Reference: [Factory Python SDK's native protocol and resource methods](https://github.com/Factory-AI/droid-sdk-python).
