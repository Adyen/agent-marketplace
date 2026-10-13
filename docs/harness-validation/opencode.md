# OpenCode harness compatibility

OpenCode is an opt-in source and target (`opencode`). Installation, conversion,
inspection, and repair require only the Go binary. Runtime hook compatibility
was executed against OpenCode **1.18.34**, Linux arm64; the reproducible gate and
executable checksums are in [the probe guide](../../tools/opencode-probe/README.md).

## Native resources

| Kind | Supported representation | Explicit limits |
|---|---|---|
| Skill | `skills/<name>/SKILL.md`, body, support files, description, license, compatibility, metadata | Execution restrictions unsupported by native skills produce `skill.opencode.security-restriction-unsupported` |
| Command | `commands/<name>.md`, description, body, argument placeholders, native agent/model/subtask selections | Unrepresentable restrictions produce `command.opencode.restriction-unsupported` |
| Subagent | `agents/<name>.md`, subagent mode, model, ordered permission rules, tools, hidden, temperature, steps | Unknown mappings and unsupported isolation/nesting controls produce `subagent.opencode.restriction-unsupported`; built-in/config agent collisions are refused |
| MCP | Native local command arrays/environment or remote URL/headers; enabled, timeout, supported OAuth fields | Unrepresentable combinations produce `mcp.opencode.unrepresentable` |
| Hook | Fixed plugin plus versioned JSON command declarations | Only the command `PreToolUse` subset below |
| Rule | No output | `rule.opencode.unsupported` warning |

Plural native directories win over singular legacy aliases for the same file.
Distinct files with ambiguous resource identities are refused. YAML and JSONC
reject duplicate or malformed modeled fields and unsupported YAML merge fields.
Inline config agents/commands are diagnosed as unsupported, and Markdown
identities they override are omitted rather than converted with lost settings.
Hook declarations require strict JSON; comments and trailing commas are refused.
Individual native reads are
limited to 4 MiB. Unsupported plugins produce `ingest.opencode.plugin-unsupported`
without being imported. Other decoding and read failures use
`ingest.opencode.invalid-resource` and `ingest.opencode.read-refused`.

Native MCP `{env:NAME}` and `{file:path}` references remain data during ingestion;
Marketplace never expands them using the operator's environment or filesystem.
Canonical `${NAME}`, `${env:NAME}`, and `${file:path}` references translate to
native references. Native-looking literals without explicit reference intent
are refused. Unsupported native execution fields also block conversion to
another target instead of being silently discarded. Benign metadata loss uses
`compile.opencode.field-dropped` warnings.

## Command hooks

The fixed `agent-marketplace/opencode-hook-v1` adapter subscribes to
`tool.execute.before`. Declarations are JSON data in `hooks/agent-marketplace`,
outside the native `plugins` autoload directory. They run in filename order.
The adapter is copied as a regular file even in symlink mode.

Matchers accept an empty value or `*`, or canonical ordinary tool names joined
with `|`: `Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`, `WebFetch`, `WebSearch`,
`TodoWrite`, and `Skill`. The ordinary native callback was executed with Bash
allow/block, a nonmatching Read control, and matching TodoWrite. MCP and nested subagent execution
coverage is not advertised; explicit MCP and Task matchers are rejected.
Other lifecycle events, regex matchers, prompt handlers, input mutation, and
additional-context output are unsupported.

Each command runs through `/bin/sh -c` with the native workspace as its current
working directory and inherits the native process environment. Standard input
is one JSON object with `hook_event_name`, `session_id`, `tool_use_id`, `cwd`,
`tool_name`, and `tool_input`. Empty successful output permits the tool. A JSON
object may permit the tool or block it through `continue: false`,
`decision: "block"`, or a non-allow `hookSpecificOutput.permissionDecision`.
A `hookSpecificOutput` object must name `PreToolUse`. Missing executables,
nonzero exits, malformed output, and timeouts block active callbacks.

| Runner bound | Value |
|---|---|
| Default handler deadline | 60 seconds |
| Explicit handler deadline | 1–600 seconds |
| Total event deadline | 600 seconds |
| Declarations per tool event | 64 |
| Serialized input | 1 MiB |
| Combined stdout/stderr per handler | 1 MiB |

Timeout and failure terminate the handler process group. Forced termination of
the native parent process is outside this adapter's cleanup guarantee. Native
execution tests cover missing/nonzero/malformed handlers, timeout, output size
below/at/above the limit, declaration count 63/64/65, installed output in both
modes, removal of a sibling hook, native permission denial, and coexistence with
a handwritten plugin. Input/event bounds are enforced by the adapter; the gate
does not wait ten minutes or send a 1 MiB model-generated tool argument.

OpenCode's native plugin behavior is part of this contract: absent, disabled
(`OPENCODE_PURE`), or initialization-failed adapters are **inactive** and do not
block the tool. Active hooks do not grant permissions or manage provider/auth
configuration. This integration does not provide a global enforcement policy.
Required hook selections are therefore refused by synchronization and checking
with `hook.opencode.unsupported`, rather than reported as successfully enforced.
The affected installed generation is retained. Ordinary opt-in hooks remain
supported under the active-plugin contract.

## Destinations and ownership

Defaults are repository `.opencode` resources with repository `opencode.json`,
and machine resources/configuration under `$XDG_CONFIG_HOME/opencode` or
`~/.config/opencode`. Existing JSONC is retained. Existing native configuration
in `.opencode` takes precedence over the repository file. Absolute
`OPENCODE_CONFIG_DIR` selects resources and `OPENCODE_CONFIG` selects a separate
configuration file. Relative overrides are rejected. Repository and user
selections sharing a custom native destination are refused with
`materialize.destination.overlap` before publication.

MCP reconciliation uses the existing durable ownership, mandatory backups,
locking, journal recovery, and formatting-preserving JSONC engine. Handwritten
entries and unrelated settings survive; missing ownership preserves existing
entries and refuses collisions. Native JSONC accepts trailing commas. New MCP
files and declarations use mode `0600`; existing file modes and no-op mtimes
are retained. Check/status/doctor inspection remains read-only. Repair can adopt
an exact match with `--yes`; replacement or removal requires interactive consent.
Direct conversion refuses a destination with existing native shared configuration.

## Validation

Run affected Go tests, `make validate`, `make spec-check`, the combined
`make validate-e2e` gate, and `make tui-qa`. The independent native gate is:

```sh
OPENCODE_PROBE_BINARY=/absolute/path/to/opencode make opencode-probe
```

The conversion matrix covers all eight sources and targets. Independent native
fixtures and consumer tests supplement path-level matrix expectations.
