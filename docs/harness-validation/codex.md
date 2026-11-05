# Codex validation

[Shared validation contract](README.md)

## Installed resources

Codex splits its resources across two native partitions:

| Resource | Repository | User |
|---|---|---|
| Skills and converted command skills | `.agents/skills/<name>/SKILL.md` | `~/.agents/skills/<name>/SKILL.md` |
| Subagents | `.codex/agents/<name>.toml` | `$CODEX_HOME/agents/<name>.toml` |
| Hooks | `.codex/hooks.json` | `$CODEX_HOME/hooks.json` |
| MCP servers | `.codex/config.toml` | `$CODEX_HOME/config.toml` |

Repository paths are relative to the disposable workspace. In CI, the driver
sets `CODEX_HOME` to the isolated HOME's `.codex` directory. In the
application, `CODEX_HOME` relocates the machine Codex partition but never the
skills partition. It must be an absolute path (a relative value is an error),
and when it is unset the machine partition is `~/.codex`.

Hook entries merge under the `hooks` member of `hooks.json`. Repository support
files install under `<repo>/.codex/hook-support/<source>/<hook>/`; user support
files install under `$CODEX_HOME/hook-support/<source>/<hook>/`, or
`~/.codex/hook-support/<source>/<hook>/` when `CODEX_HOME` is unset. Codex
`SessionEnd` is a command-only event in the shipped catalog,
with a one-to-three-second timeout bound. New hook entries carry no `_managed_by`
marker; durable ownership state decides which entries sync may change.

## Native app-server inventories

The driver launches `codex app-server` and exchanges newline-delimited
protocol messages over stdin and stdout. The handshake is `initialize`
followed by `initialized`. The driver then sends one read-only inventory
request and closes input. It never sends `thread/start`, `turn/start`, or a
user message.

### Skills and converted commands

The driver requests `skills/list` with `cwds` containing only the fixture
workspace and `forceReload: true`.

Before installation, neither fixture entry may appear. After installation,
each expected entry must appear exactly once and have:

- the fixture name and description;
- `enabled: true`;
- the expected `repo` or `user` scope;
- the installed `SKILL.md` path or its actual symlink-resolved source.

The returned workspace must match the request, and the resource error
inventory must be empty. A name that appears in loading errors does not count
as discovery.

### Hooks

The driver requests `hooks/list` for the same workspace. It matches the two
fixture handlers by their exact command payloads, then verifies the native
event name, command-handler type, matcher, and source file.

Codex lists hooks even when they are not approved to run. This check does not
enable or execute them and does not approve hook hashes. A successful
`hooks/list` is not evidence of execution. The shared fixture separately
asserts that its hook marker was not written.

Implementation: [protocol transport](../../e2e/harness_compatibility_protocol_test.go)
and [typed inventory checks](../../e2e/harness_compatibility_inventory_test.go).

## MCP servers and trust

The driver runs:

```text
codex --version
codex mcp list --json
codex mcp get <fixture-name> --json
```

The fixture names must be absent before materialization. Repository-scoped MCP
entries must also stay absent after installation while the workspace is
untrusted. Only then does the test add one trust record for the disposable
workspace to its isolated user config. It repeats list and get and requires
both names, the executable path, the label arguments, and the fixture
environment key.

User-scoped MCP inspection does not need this repository trust step. Agent
Marketplace never writes trust state. Both scopes finish with `sync --check`.

The MCP connection status is `not-probed`: list and get certify that Codex
recognizes the configuration, not a transport handshake or tool call.

## Remaining limits

- Subagents: `codex agents` lists running sessions, not configured custom
  agent definitions, and the inspected app-server inventories do not list
  custom definitions either. The generated TOML and its compilation are still
  checked, without any claim of native agent invocation.
- Rules: outside the compiler's Codex scope by design; the test asserts the
  omission diagnostic.
- Collections: virtual selections whose members are validated individually.

Reference: [Codex app-server protocol, including skills/list and hooks/list](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server/README.md).
