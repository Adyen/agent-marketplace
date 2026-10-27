## Why

Claude Code reads every `~/.claude` path, and `~/.claude.json`, from
`CLAUDE_CONFIG_DIR` when that variable is set. Its documentation recommends one
configuration directory per account and one per dev container. Agent
Marketplace ignores the variable: a machine-scope install writes to `~/.claude`
and `~/.claude.json`, so the account Claude Code actually runs with never sees
the resources and the default account receives them instead (issue #920).
`CODEX_HOME` and `OPENCODE_CONFIG_DIR` already relocate their harness targets'
machine roots; Claude Code is the only harness target with a documented
configuration-directory variable that is not honored.

## What Changes

- When `CLAUDE_CONFIG_DIR` is an absolute path, the Claude Code machine root is
  that directory. Machine-scope skills, commands, subagents, hook support,
  `.claude-plugin/plugin.json`, `settings.json` hooks and their legacy
  `hooks/hooks.json` go under it, and machine-scope MCP servers go to
  `$CLAUDE_CONFIG_DIR/.claude.json` instead of `~/.claude.json`. The directory
  is created when missing.
- `sync`, `sync --check`, `install`, `remove`, `update`, `status`, `doctor`,
  `backup`, `backup restore`, and harness detection (`init` and the TUI
  first-run dialog) resolve the same relocated root. `~/.claude` and
  `~/.claude.json` are neither read nor written while the variable is set;
  output generated there earlier is left in place.
- A relative value refuses machine-scope Claude Code work and the `status` and
  `doctor` root summaries with `CLAUDE_CONFIG_DIR must be absolute`, as
  `CODEX_HOME` and `OPENCODE_CONFIG_DIR` do. Repository-only Claude Code output
  and other harness targets are unaffected. An unset or empty variable changes
  nothing.
- Because `.claude.json` now lives inside the machine root, cleanup of that
  root explicitly keeps the file: only owned `mcpServers` entries are ever
  added, updated, or removed, with the same preservation and fail-closed rules
  as `~/.claude.json` today.
- When the relocated machine root and `<repo>/.claude` name the same location,
  or one contains the other, including case- or normalization-only spellings,
  and both scopes produce Claude Code output, materialization refuses with
  `materialize.destination.overlap` before any Claude Code write. Disjoint
  roots support both scopes normally.
- Repository-scope destinations do not change.

## Capabilities

### New Capabilities

- `claude-native-configuration`: Claude Code machine destinations under
  `CLAUDE_CONFIG_DIR`, preservation of the relocated `.claude.json`, and
  overlapping-scope refusal.

### Modified Capabilities

- `materialization`: root admission names the relocated Claude Code machine
  root beside `CODEX_HOME`; relative `CLAUDE_CONFIG_DIR` joins relative
  `CODEX_HOME` as a hard error in `status` and `doctor` root summaries.

## Impact

Harness destination resolution for the Claude Code target (legacy single-root
path), the Claude MCP aggregate root, the prune fence around aggregates that
live inside their root, backup managed-path collection and archive mapping for
Claude Code, a Claude Code scope-overlap preflight, and harness detection.
Process-isolated command tests, E2E, and TUI QA sandboxes gain
`CLAUDE_CONFIG_DIR` isolation. Documentation: harness roots and output paths,
environment variables, `AGENTS.md`, `CODING_STANDARDS.md`. No consumer
configuration, lock, API, or native schema changes. No change to Codex,
OpenCode, or any other harness target.

## Release note

Agent Marketplace now honors `CLAUDE_CONFIG_DIR`: machine-scope Claude Code
resources and MCP servers are installed under that directory and its
`.claude.json` when the variable is set to an absolute path. A relative value
is an error; an unset variable keeps `~/.claude` and `~/.claude.json`.
