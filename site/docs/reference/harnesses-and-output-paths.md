# Harnesses and output paths reference

This page lists the [harnesses](../glossary.md#harness) Agent Marketplace supports and the exact files `sync` writes for each one. Use it to find where an installed skill, command, subagent, hook, or MCP server ended up, or to decide which paths to add to `.gitignore`.

The roots and per-kind paths come from `pkg/harness-compilation/target` and `internal/harness/roots.go` in the source code.

## Supported harness identifiers

| Harness | Identifier |
|---|---|
| Claude Code | `claude` |
| Factory Droid | `factory` |
| Cursor | `cursor` |
| Windsurf | `windsurf` |
| Gemini CLI | `gemini` |
| Devin CLI | `devin` |
| Codex | `codex` |
| OpenCode | `opencode` |

These identifiers appear in `harnesses:` lists and in `source_harness:`. Identifiers are lowercase. An unknown identifier in a v4 configuration fails with `v4.harness`.

## Roots

Each harness has a repository root and a machine root. Resources selected by the repository and personal-local files go under the repository root. Resources selected by the user and organization files go under the machine root, which defaults to a directory in your home directory.

| Harness | Repository root | Machine root |
|---|---|---|
| Claude Code | `<repo>/.claude` | `$CLAUDE_CONFIG_DIR` or `~/.claude` |
| Factory Droid | `<repo>/.factory` | `~/.factory` |
| Cursor | `<repo>/.cursor` | `~/.cursor` |
| Windsurf | `<repo>` | `~/.codeium/windsurf` |
| Gemini CLI | `<repo>/.gemini` | `~/.gemini` |
| Devin CLI | `<repo>/.devin` | `~/.config/devin` |
| Codex | `<repo>/.agents` (skills, commands) and `<repo>/.codex` (subagents, hooks, mcp) | `~/.agents` (skills, commands) and `~/.codex` or `$CODEX_HOME` (subagents, hooks, mcp) |
| OpenCode | `<repo>/.opencode` plus native repository configuration | `$XDG_CONFIG_HOME/opencode` or `~/.config/opencode` |

Codex splits its files across two roots. When `CODEX_HOME` is set, it replaces `~/.codex` for machine subagents, hooks, and MCP servers. It never moves skills, which stay in `~/.agents`. `CODEX_HOME` must be an absolute path; a relative value stops the run with an error.

Claude Code MCP servers go to `<repo>/.mcp.json` at repository scope. At machine scope they go to the `mcpServers` member of `$CLAUDE_CONFIG_DIR/.claude.json` when the variable is nonempty, or `~/.claude.json` otherwise.

`CLAUDE_CONFIG_DIR` must be absolute. It relocates all Claude Code machine
resources, including hooks and hook support; repository paths do not change.
An unset or empty value keeps the default layout. Existing output in the
default account is not moved or pruned when a different directory is selected.
Cleanup only inspects Claude's declared aggregate files, not unrelated JSON
files or other accounts beneath the relocated root. Admitted directory aliases
also support native hooks in both materialization modes.
Keep the relocated machine root separate from `<repo>/.claude`: when both
scopes produce output and their roots overlap, sync refuses with
`materialize.destination.overlap`.
Git administrative paths are not eligible: this includes `.git`, configured
Git/common directories, linked-worktree metadata, and aliases into them.

## Output paths by harness

In the tables below, a skill path ending in `SKILL.md` means the skill is a folder named `<name>` that contains `SKILL.md` and any files that come with it. A path followed by "under `key`" is one member of a shared JSON file.

### Claude Code

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.claude/skills/<name>/SKILL.md` | `~/.claude/skills/<name>/SKILL.md` |
| command | `.claude/commands/<name>.md` | `~/.claude/commands/<name>.md` |
| subagent | `.claude/agents/<name>.md` | `~/.claude/agents/<name>.md` |
| hook | `.claude/settings.json` under `hooks` | `~/.claude/settings.json` under `hooks` |
| mcp | `<repo>/.mcp.json` | `~/.claude.json` under `mcpServers` |

Sync also lists installed skills and subagents in `.claude/.claude-plugin/plugin.json` (machine: `~/.claude/.claude-plugin/plugin.json`), under `skills` and `agents`.

The machine paths in this table show the default layout. With a nonempty
`CLAUDE_CONFIG_DIR`, replace `~/.claude` with that directory and
`~/.claude.json` with `$CLAUDE_CONFIG_DIR/.claude.json`. Detection, status,
doctor, backup, and restore use the same effective machine paths.

### Codex

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.agents/skills/<name>/SKILL.md` | `~/.agents/skills/<name>/SKILL.md` |
| command | `.agents/skills/<name>/SKILL.md` (with `x-agent-marketplace-kind: command`) | `~/.agents/skills/<name>/SKILL.md` |
| subagent | `.codex/agents/<name>.toml` | `~/.codex/agents/<name>.toml` or `$CODEX_HOME/agents/<name>.toml` |
| hook | `.codex/hooks.json` | `~/.codex/hooks.json` or `$CODEX_HOME/hooks.json` |
| mcp | `.codex/config.toml` under `mcp_servers` | `~/.codex/config.toml` or `$CODEX_HOME/config.toml` under `mcp_servers` |

Codex has no separate command format, so each command is installed as a skill whose `SKILL.md` front matter carries `x-agent-marketplace-kind: command`.

When sync changes `.codex/hooks.json` or `.codex/config.toml` in a repository, it prints `materialize.codex.trust-review` because Codex may ask you to review the changed hooks or MCP servers before it uses them. `sync --check` prints `materialize.codex.trust-review-pending` when such a change is waiting. Agent Marketplace never edits Codex trust settings and never adds Codex paths to `.gitignore`.

### Cursor

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.cursor/skills/<name>/SKILL.md` | `~/.cursor/skills/<name>/SKILL.md` |
| command | `.cursor/commands/<name>.md` | `~/.cursor/commands/<name>.md` |
| subagent | `.cursor/agents/<name>.md` | `~/.cursor/agents/<name>.md` |
| hook | `.cursor/hooks.json` under `hooks`, with `version: 1` | `~/.cursor/hooks.json` under `hooks`, with `version: 1` |
| mcp | `.cursor/mcp.json` | `~/.cursor/mcp.json` |

### Devin CLI

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.devin/skills/<name>/SKILL.md` | `~/.config/devin/skills/<name>/SKILL.md` |
| command | `.devin/skills/<name>/SKILL.md` (with `x-agent-marketplace-kind: command`) | `~/.config/devin/skills/<name>/SKILL.md` |
| subagent | `.devin/agents/<name>.md` | `~/.config/devin/agents/<name>.md` |
| hook | `.devin/hooks.v1.json` | `~/.config/devin/config.json` under `hooks` |
| mcp | `.devin/mcp_config.json` | `~/.config/devin/mcp_config.json` |

Like Codex, Devin receives commands as skills marked with `x-agent-marketplace-kind: command`.

### Factory Droid

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.factory/skills/<name>/SKILL.md` | `~/.factory/skills/<name>/SKILL.md` |
| command | `.factory/commands/<name>.md` (copied as a regular file) | `~/.factory/commands/<name>.md` |
| subagent | `.factory/droids/<name>.md` | `~/.factory/droids/<name>.md` |
| hook | `.factory/hooks.json` | `~/.factory/hooks.json` |
| mcp | `.factory/mcp.json` | `~/.factory/mcp.json` |

Factory commands are always regular files, even in symlink mode, because the Droid CLI does not follow symlinked command files.

### Gemini CLI

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.gemini/skills/<name>/SKILL.md` | `~/.gemini/skills/<name>/SKILL.md` |
| command | `.gemini/commands/<name>.toml` | `~/.gemini/commands/<name>.toml` |
| subagent | `.gemini/agents/<name>.md` | `~/.gemini/agents/<name>.md` |
| hook | `.gemini/settings.json` under `hooks` | `~/.gemini/settings.json` under `hooks` |
| mcp | `.gemini/settings.json` under `mcpServers` | `~/.gemini/settings.json` under `mcpServers` |

Hooks and MCP servers share one `settings.json` per scope. The Gemini compiler also produces a `gemini-extension.json` file, but sync does not install it. Its `mcpServers` entries go into `settings.json` instead.

### OpenCode

OpenCode is opt-in. Select `opencode` through the harness modal or configuration.

| Kind | Repository path | Machine path relative to its root |
|---|---|---|
| skill | `.opencode/skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` |
| command | `.opencode/commands/<name>.md` | `commands/<name>.md` |
| subagent | `.opencode/agents/<name>.md` | `agents/<name>.md` |
| hook | `.opencode/hooks/agent-marketplace/<name>.json` | `hooks/agent-marketplace/<name>.json` |
| hook support | `.opencode/hook-support/<source>/<hook>/` | `hook-support/<source>/<hook>/` |
| hook adapter | `.opencode/plugins/agent-marketplace.js` | `plugins/agent-marketplace.js` |
| mcp | `opencode.json` or existing `opencode.jsonc`, under `mcp` | `opencode.json` or existing `opencode.jsonc`, under `mcp` |

The machine root follows `XDG_CONFIG_HOME`, falling back to `~/.config/opencode`.
Absolute `OPENCODE_CONFIG_DIR` and `OPENCODE_CONFIG` override the resource
folder and configuration file separately. Native configuration already in
`.opencode` takes precedence over the repository root configuration.

Commands stay commands, and agents use native subagent mode. The fixed hook
adapter is a regular file in both materialization modes. Supported command
`PreToolUse` hooks run through native plugins; absent, disabled, or failed
plugins are inactive. Hook support is installed under `hook-support/`, and the
adapter gives handlers `AGENT_MARKETPLACE_PROJECT_DIR` so repository commands
reach it from any working directory. Rules and arbitrary executable plugin
conversion are unsupported. For protocol details, see the repository's OpenCode compatibility
note under `docs/harness-validation/opencode.md`.

### Windsurf

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.windsurf/skills/<name>/SKILL.md` | `~/.codeium/windsurf/skills/<name>/SKILL.md` |
| command | `.windsurf/workflows/<name>.md` | `~/.codeium/windsurf/global_workflows/<name>.md` |
| hook | `.devin/hooks.json` under `hooks` | `~/.codeium/windsurf/hooks.json` under `hooks` |
| mcp | `.windsurf/mcp_config.json` | `~/.codeium/windsurf/mcp_config.json` |

Files that a hook needs, such as the script it runs, go to `hook-support/<source>/<hook>/` below the scope's target root. For Windsurf, that means repository paths below `hook-support/` because the repository itself is the root, and machine paths below `~/.codeium/windsurf/hook-support/`. An existing `.windsurf/hooks/` folder (machine: `~/.codeium/windsurf/hooks/`) from older output is left untouched.

Windsurf has no subagents. Sync skips them for this harness and prints `subagent.windsurf.no-primitive`.

The Windsurf repository root is the repository itself. Repository hooks therefore use `.devin/hooks.json`, while Devin CLI repository hooks use the separate `.devin/hooks.v1.json`. The machine root is already `~/.codeium/windsurf`, so machine paths have no such prefix, and machine commands go to `global_workflows` because that is where Windsurf looks for machine-wide workflows.

## Sync output and legacy hooks

The hook rows above are the files `sync` materializes for each target.

During a mutating sync, sync moves only entries marked `_managed_by: agent-marketplace` out of the declared legacy hook files: `hooks/hooks.json` below the Claude, Cursor, and Factory roots in both scopes, and repository-scope `.windsurf/hooks.json` for Windsurf. Handwritten entries stay behind, and a malformed legacy file stops that target's migration with `materialize.hook.legacy-malformed` before either file changes. Gemini has a separate bounded migration for entries a v3.1.1 installation marked in the older flat settings shape; it moves them into the grouped `hooks` shape without adopting unmarked entries.

## Symlinks and regular files

In the default `symlink` mode, skills, commands, and subagents are symlinks into the compiled cache at `~/.cache/agent-marketplace/compiled/<harness>/`. Factory commands are the exception and are always copied. In `file` mode, sync writes regular files and folders for every harness. See [How to write regular files instead of symlinks](../how-to/write-regular-files-instead-of-symlinks.md).

Shared files, such as `hooks.json`, `.mcp.json`, `settings.json`, `.codex/config.toml`, and `.devin/mcp_config.json`, hold entries from several sources as well as entries you wrote yourself. They are always regular files in both modes. Sync edits them in place, adds and removes only the entries it installed, and keeps your own entries, comments, file modes, and unrelated settings.

Hook entries in the seven native hook files carry no `_managed_by` marker. Sync records owned hook entries in state files under `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`). If that record is missing or damaged, sync treats every existing native hook entry as yours, removes nothing, and refuses to overwrite an entry it cannot prove it owns. A handwritten entry that matches a selected hook satisfies it without being adopted. Deselection removes only entries that ownership state proves sync installed, along with their owned support files. Recognized marked legacy entries are handled separately under the marker authority described above. MCP server ownership is unchanged by hook migration; see the [filesystem layout reference](./filesystem-layout.md) for the ownership state locations.

OpenCode has no shared hook file: each hook is its own declaration file next to the fixed adapter. OpenCode MCP entries in `opencode.json` or `opencode.jsonc` carry no marker; sync records them in the same ownership state and treats a missing or damaged record the same way.

## Related documentation

- [How to configure multiple harnesses](../how-to/configure-multiple-harnesses.md)
- [How to write regular files instead of symlinks](../how-to/write-regular-files-instead-of-symlinks.md)
- [Filesystem layout reference](./filesystem-layout.md)
