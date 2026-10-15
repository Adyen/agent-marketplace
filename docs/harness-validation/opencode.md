# OpenCode harness compatibility

OpenCode is an opt-in source and target (`opencode`). Installation, conversion,
inspection, and repair require only the Go binary. Runtime hook compatibility
was executed against OpenCode **1.18.34**, Linux arm64; the reproducible gate and
executable checksums are in [the probe guide](../../tools/opencode-probe/README.md).

## Native resources

| Kind | Supported representation | Explicit limits |
|---|---|---|
| Skill | `skills/<name>/SKILL.md`, body, support files, description, license, compatibility, metadata | Execution restrictions unsupported by native skills produce `skill.opencode.security-restriction-unsupported` |
| Command | `commands/<name>.md`, description, body, argument placeholders, native agent/model/subtask selections | Unrepresentable restrictions produce `command.opencode.restriction-unsupported`; canonical models without a catalog identifier produce `model.unresolved` |
| Subagent | `agents/<name>.md`, subagent mode, model, ordered permission rules, tools, hidden, temperature, steps | Tool lists that do not map onto whole permission keys, unknown mappings, and unsupported isolation/nesting controls produce `subagent.opencode.restriction-unsupported`; built-in/config agent collisions are refused |
| MCP | Native local command arrays/environment or remote URL/headers; enabled, timeout, supported OAuth fields | Unrepresentable combinations produce `mcp.opencode.unrepresentable` |
| Hook | Fixed plugin plus versioned JSON command declarations | Only the command `PreToolUse` subset below |
| Rule | No output | `rule.opencode.unsupported` warning |

Autodetection treats `.opencode/` as a source marker only when it holds
OpenCode resources, plugins, hook declarations, or `opencode.json[c]`; a
dependency-only directory is ignored. A source whose otherwise-detected layout
has its own workspace folder (`.claude/`, `.codex/`, `.factory/`, `.cursor/`,
`.gemini/`, `.windsurf/`) keeps that layout, and OpenCode wins over shared
skills locations. Coexisting layouts report `ingest.autodetect.ambiguous`;
`source_harness` chooses explicitly.

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

Models, tools, and hook events translate through the shipped OpenCode catalogs
in `pkg/harness-compilation/{modelpolicy,toolpolicy,hookpolicy}/catalogs`.
Native provider-qualified models keep their spelling; canonical models without
an OpenCode catalog identifier are omitted with `model.unresolved`. Agent
permission keys group `edit`, `write`, and `apply_patch` under `edit`, so a
tool list is translated only when it maps onto whole permission keys: `Edit`
and `Write` together become `edit`, while either one alone refuses the agent
instead of producing an unenforced `write` rule or a broader `edit` rule.

## Command hooks

The fixed `agent-marketplace/opencode-hook-v1` adapter subscribes to
`tool.execute.before`. Declarations are JSON data in `hooks/agent-marketplace`,
outside the native `plugins` autoload directory. They run in filename order.
The adapter is copied as a regular file even in symlink mode.

Hooks from any source layout translate through the hook and tool catalogs.
Declaration matchers are an empty value, `*`, or exact tool names joined with
`|`, spelled as the adapter compares them: `Bash`, `Read`, `Write`, `Edit`,
`Glob`, `Grep`, `WebFetch`, `WebSearch`, `TodoWrite`, and `Skill` for the IDs it
canonicalizes, and the native ID for `apply_patch` and `question`. A matcher is
emitted only when it covers exactly the source tool set. Because `apply_patch`
also writes and edits files, `Edit` or `Write` alone is refused; `Edit|Write`
becomes `Write|Edit|apply_patch`. The ordinary native callback was executed
with Bash allow/block, a nonmatching Read control, and matching TodoWrite. MCP
and nested subagent execution coverage is not advertised; explicit MCP and Task
matchers are rejected. Other lifecycle events, regex matchers, prompt handlers,
input mutation, and additional-context output are unsupported. Ingested
declarations with regex-like, comma-separated, or adapter-rewritten native
matchers are refused because the adapter would read them differently.

Each hook's support files (the regular files under its source definition's
directory, excluding the definition and the JSON declarations of an OpenCode
declaration directory) are installed under
`hook-support/<source>/<hook>/` in the OpenCode root, as portable regular
copies at repository scope in both modes. They are published before the
declarations that reference them and pruned with their hook. Unsafe, replaced,
or conflicting support refuses the hook with `materialize.hook.support-refused`
or `materialize.hook.support-conflict` and keeps the installed generation.

The adapter sets `AGENT_MARKETPLACE_PROJECT_DIR` for every handler: the
directory containing its `.opencode` root, or the instance directory for an
adapter in any other configuration directory. Repository-scope
`${CLAUDE_PLUGIN_ROOT}` references become
`"$AGENT_MARKETPLACE_PROJECT_DIR/.opencode/hook-support/<source>/<hook>/..."`,
so they work when OpenCode starts in a subdirectory. Machine-scope references,
and those of a repository selection relocated by `OPENCODE_CONFIG_DIR`, use the
absolute support path. Project-directory variables translate to
`AGENT_MARKETPLACE_PROJECT_DIR`. Commands never point into the registry cache.

Sync lists added, replaced, and removed declarations in its hook review with
`materialize.hook.review`; `sync --check` lists pending declaration changes
with `materialize.hook.review-pending`, writes nothing, and exits nonzero.

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
modes, removal of a sibling hook, installed hook support reached from the
workspace and a subdirectory, native permission denial, and coexistence with
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
