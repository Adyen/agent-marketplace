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

These identifiers appear in `harnesses:` lists, in `source_harness:`, and with `convert --from` and `convert --to`. Identifiers are lowercase. An unknown identifier in a v4 configuration fails with `v4.harness`, and `convert` rejects one with an error that lists the valid names.

## Roots

Each harness has a repository root and a machine root. Resources selected by the repository and personal-local files go under the repository root. Resources selected by the user and organization files go under the machine root in your home directory.

| Harness | Repository root | Machine root |
|---|---|---|
| Claude Code | `<repo>/.claude` | `~/.claude` |
| Factory Droid | `<repo>/.factory` | `~/.factory` |
| Cursor | `<repo>/.cursor` | `~/.cursor` |
| Windsurf | `<repo>` | `~/.codeium/windsurf` |
| Gemini CLI | `<repo>/.gemini` | `~/.gemini` |
| Devin CLI | `<repo>/.devin` | `~/.config/devin` |
| Codex | `<repo>/.agents` (skills, commands) and `<repo>/.codex` (subagents, hooks, mcp) | `~/.agents` (skills, commands) and `~/.codex` or `$CODEX_HOME` (subagents, hooks, mcp) |

Codex splits its files across two roots. When `CODEX_HOME` is set, it replaces `~/.codex` for machine subagents, hooks, and MCP servers. It never moves skills, which stay in `~/.agents`. `CODEX_HOME` must be an absolute path; a relative value stops the run with an error.

Claude Code MCP servers are the one exception to these roots. They go to `<repo>/.mcp.json` or to the `mcpServers` member of `~/.claude.json`.

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

## Sync output, conversion output, and legacy hooks

The hook rows above are the files `sync` materializes for each target. A pure `convert` bundle may still contain a bundle-layout `hooks/hooks.json`; that file is conversion output, not a native sync destination.

During a mutating sync, sync moves only entries marked `_managed_by: agent-marketplace` out of the declared legacy hook files: `hooks/hooks.json` below the Claude, Cursor, and Factory roots in both scopes, and repository-scope `.windsurf/hooks.json` for Windsurf. Handwritten entries stay behind, and a malformed legacy file stops that target's migration with `materialize.hook.legacy-malformed` before either file changes. Gemini has a separate bounded migration for entries a v3.1.1 installation marked in the older flat settings shape; it moves them into the grouped `hooks` shape without adopting unmarked entries.

## Symlinks and regular files

In the default `symlink` mode, skills, commands, and subagents are symlinks into the compiled cache at `~/.cache/agent-marketplace/compiled/<harness>/`. Factory commands are the exception and are always copied. In `file` mode, sync writes regular files and folders for every harness. See [How to write regular files instead of symlinks](../how-to/write-regular-files-instead-of-symlinks.md).

Shared files, such as `hooks.json`, `.mcp.json`, `settings.json`, `.codex/config.toml`, and `.devin/mcp_config.json`, hold entries from several sources as well as entries you wrote yourself. They are always regular files in both modes. Sync edits them in place, adds and removes only the entries it installed, and keeps your own entries, comments, file modes, and unrelated settings.

Hook entries on all seven harnesses carry no `_managed_by` marker. Sync records owned hook entries in state files under `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`). If that record is missing or damaged, sync treats every existing native hook entry as yours, removes nothing, and refuses to overwrite an entry it cannot prove it owns. A handwritten entry that matches a selected hook satisfies it without being adopted. Deselection removes only entries that ownership state proves sync installed, along with their owned support files. Recognized marked legacy entries are handled separately under the marker authority described above. MCP server ownership is unchanged by hook migration; see the [filesystem layout reference](./filesystem-layout.md) for the ownership state locations.

## Related documentation

- [How to configure multiple harnesses](../how-to/configure-multiple-harnesses.md)
- [How to write regular files instead of symlinks](../how-to/write-regular-files-instead-of-symlinks.md)
- [Filesystem layout reference](./filesystem-layout.md)
