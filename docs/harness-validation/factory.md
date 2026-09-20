# Factory Droid validation

[Shared validation contract](README.md)

## Installed resources

The fixture uses the scope's `.factory` directory: skills under `skills/`, commands under `commands/`, droids under `droids/`, and MCP entries in `mcp.json`. Both repository/user scopes and symlink/file modes are tested. Native inspection reads this output directly; it does not register another plugin or force a different materialization mode.

## Skills, commands, and droids: native resource RPCs

The driver launches the pinned executable as:

```text
droid exec --input-format stream-jsonrpc --output-format stream-jsonrpc
```

It sends `droid.initialize_session` with the disposable workspace and a fixture machine identifier. This initializes the metadata session without sending a user message. Factory's envelope includes `type: "request"`, `factoryApiVersion`, `factoryProtocolVersion`, and a string request ID; bare numeric-ID JSON-RPC requests are not equivalent.

After successful initialization, the driver calls one resource method:

| Resource | RPC | Assertions |
|---|---|---|
| Skill | `droid.list_skills` | Exact name/description, enabled state, project/personal location, installed or symlink-resolved file path |
| Command | `droid.list_commands` | Exact fixture name and description |
| Subagent/droid | `droid.get_context_breakdown` | Exact fixture name in the `droids` inventory and expected location; token estimates are not treated as execution evidence |

Each inventory must omit the fixture before installation and contain it exactly once afterward. A process error, RPC rejection, missing inventory, or mismatched field fails the resource probe. No user-message RPC, model request, permission bypass, or inline replacement droid is sent.

Factory returns built-in skill bodies with its skill inventory. Its protocol response has a separate 10 MiB bound, matching the SDK transport's documented multi-megabyte response support; stderr remains bounded to the normal diagnostic limit. Tests cover both valid larger content and an over-limit response. Bodies are not copied into compatibility reports.

Implementation: [protocol transport](../../e2e/harness_compatibility_protocol_test.go) and [inventory validation](../../e2e/harness_compatibility_inventory_test.go).

## MCP servers

`droid mcp list` must omit both fixture names before installation and show them after `sync`. This is configuration-recognition evidence; transport remains `not-probed`. It is not a claim that a model invoked either server.

## Remaining limits

- **Hooks:** `/hooks` is an interactive session manager. The documented resource RPC surface does not expose an installed-hook inventory.
- **Rules:** no named rule-file inventory is exposed by these CLI/RPC methods. Tool lists and context-token totals are not rule-discovery evidence.
- **Collections:** virtual selections; validate expanded members instead.

Keep stable fixture identities across materialization modes. Do not accept truncated names or replace a failing native probe with a file-only check.

The [Factory hook event reference](https://docs.factory.ai/harness/hooks) documents
`PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Notification`, `Stop`,
`SubagentStop`, `PreCompact`, `SessionStart`, and `SessionEnd`. Conversion retains
these events. `PermissionRequest` is not a separate documented Factory event;
permission prompts are notifications, and permission decisions belong to
`PreToolUse` output. Conversion must not substitute either lifecycle for a
`PermissionRequest` hook. Unknown events are suppressed with
`hook.event-unmappable`.

Reference: [Factory Python SDK's native protocol and resource methods](https://github.com/Factory-AI/droid-sdk-python).
