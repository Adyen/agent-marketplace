# Codex validation

[Shared validation contract](README.md)

## Installed resources

Codex has two native partitions:

| Resource | Repository | User |
|---|---|---|
| Skills and converted command skills | `.agents/skills/<name>/SKILL.md` | `~/.agents/skills/<name>/SKILL.md` |
| Subagents | `.codex/agents/<name>.toml` | `$CODEX_HOME/agents/<name>.toml` |
| Hooks | `.codex/hooks.json` | `$CODEX_HOME/hooks.json` |
| MCP servers | `.codex/config.toml` | `$CODEX_HOME/config.toml` |

Repository paths are relative to the disposable workspace. In CI, `CODEX_HOME` is explicitly set to the isolated HOME's `.codex` directory. In the application it can relocate the machine Codex partition, never the skills partition.

## Native app-server inventories

The driver launches `codex app-server` and exchanges newline-delimited protocol messages over stdin/stdout. The handshake is `initialize` followed by `initialized`. It then sends one read-only inventory request and closes input. It never sends `thread/start`, `turn/start`, or a user message.

### Skills and converted commands

Request `skills/list` with `cwds` containing only the fixture workspace and `forceReload: true`.

Before installation, neither fixture entry may appear. After installation, each expected entry must appear exactly once and have:

- the fixture name and description;
- `enabled: true`;
- the expected `repo` or `user` scope;
- the installed `SKILL.md` path or its actual symlink-resolved source.

The returned workspace must match the request and the resource error inventory must be empty. Names appearing in loading errors do not count as discovery.

### Hooks

Request `hooks/list` for the same workspace. Match the two fixture handlers by their exact command payloads, then verify the native event name, command-handler type, matcher, and source file.

Hooks are discoverable even when not approved to run. This check does not enable or execute them, does not approve hook hashes, and does not claim execution merely because `hooks/list` succeeds. The shared fixture separately asserts that its hook marker was not written.

Implementation: [protocol transport](../../e2e/harness_compatibility_protocol_test.go) and [typed inventory checks](../../e2e/harness_compatibility_inventory_test.go).

## MCP servers and trust

The driver runs:

```text
codex --version
codex mcp list --json
codex mcp get <fixture-name> --json
```

Fixture names must be absent before materialization. Repository-scoped MCP entries must also be absent after installation while the workspace remains untrusted. Only then does the test add one trust record for the disposable workspace to its isolated user config. It repeats list/get and requires both names, executable path, label arguments, and the fixture environment key.

User-scoped MCP inspection does not need this repository trust transition. Agent Marketplace never writes trust state. Both scopes finish with `sync --check`.

MCP connection status is `not-probed`: list/get certify configuration recognition, not a transport handshake or tool call.

## Remaining limits

- **Subagents:** `codex agents` lists running sessions, not configured custom agent definitions. The inspected app-server inventories do not expose a custom-definition listing. Generated TOML and compilation remain checked, without claiming native agent invocation.
- **Rules:** intentionally outside the compiler's Codex scope; the omission diagnostic is asserted.
- **Collections:** virtual selections whose members are validated individually.

Reference: [Codex app-server protocol, including skills/list and hooks/list](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server/README.md).
