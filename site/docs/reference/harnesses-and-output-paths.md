# Harnesses and output paths reference

The roots and per-kind paths are derived from `pkg/harness-compilation/target` and `internal/harness/roots.go`.

## Supported harness identifiers

| Harness | Identifier |
|---|---|
| Claude Code | `claude` |
| Codex | `codex` |
| Cursor | `cursor` |
| Devin CLI | `devin` |
| Factory Droid | `factory` |
| Gemini CLI | `gemini` |
| Windsurf | `windsurf` |

Unknown identifiers are rejected. Target names are lowercase.

## Roots

| Harness | Repository root | Machine root |
|---|---|---|
| Claude Code | `<repo>/.claude` | `~/.claude` |
| Codex | `<repo>/.agents` (skills, commands) and `<repo>/.codex` (subagents, hooks, mcp) | `~/.agents` (skills, commands) and `~/.codex` or `$CODEX_HOME` (subagents, hooks, mcp) |
| Cursor | `<repo>/.cursor` | `~/.cursor` |
| Devin CLI | `<repo>/.devin` | `~/.config/devin` |
| Factory Droid | `<repo>/.factory` | `~/.factory` |
| Gemini CLI | `<repo>/.gemini` | `~/.gemini` |
| Windsurf | `<repo>` | `~/.codeium/windsurf` |

`$CODEX_HOME` must be absolute and relocates only the `.codex` machine partition; it never moves the skill partition.

## Output paths by harness

### Claude Code

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.claude/skills/<name>/SKILL.md` | `~/.claude/skills/<name>/SKILL.md` |
| command | `.claude/commands/<name>.md` | `~/.claude/commands/<name>.md` |
| subagent | `.claude/agents/<name>.md` | `~/.claude/agents/<name>.md` |
| hook | `.claude/hooks/hooks.json` | `~/.claude/hooks/hooks.json` |
| mcp | `<repo>/.mcp.json` | `~/.claude.json` under `mcpServers` |

### Codex

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.agents/skills/<name>/SKILL.md` | `~/.agents/skills/<name>/SKILL.md` |
| command | `.agents/skills/<name>/SKILL.md` | `~/.agents/skills/<name>/SKILL.md` |
| subagent | `.codex/agents/<name>.toml` | `~/.codex/agents/<name>.toml` or `$CODEX_HOME/agents/<name>.toml` |
| hook | `.codex/hooks.json` | `~/.codex/hooks.json` or `$CODEX_HOME/hooks.json` |
| mcp | `.codex/config.toml` | `~/.codex/config.toml` or `$CODEX_HOME/config.toml` |

### Cursor

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.cursor/skills/<name>/SKILL.md` | `~/.cursor/skills/<name>/SKILL.md` |
| command | `.cursor/commands/<name>.md` | `~/.cursor/commands/<name>.md` |
| subagent | `.cursor/agents/<name>.md` | `~/.cursor/agents/<name>.md` |
| hook | `.cursor/hooks/hooks.json` | `~/.cursor/hooks/hooks.json` |
| mcp | `.cursor/mcp.json` | `~/.cursor/mcp.json` |

### Devin CLI

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.devin/skills/<name>/SKILL.md` | `~/.config/devin/skills/<name>/SKILL.md` |
| command | `.devin/skills/<name>/SKILL.md` (with `x-agent-marketplace-kind: command`) | `~/.config/devin/skills/<name>/SKILL.md` |
| subagent | `.devin/agents/<name>.md` | `~/.config/devin/agents/<name>.md` |
| hook | `.devin/hooks.v1.json` | `~/.config/devin/config.json` under `hooks` |
| mcp | `.devin/mcp_config.json` | `~/.config/devin/mcp_config.json` |

Devin aggregate ownership is stored in XDG state, not inside the generated files.

### Factory Droid

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.factory/skills/<name>/SKILL.md` | `~/.factory/skills/<name>/SKILL.md` |
| command | `.factory/commands/<name>.md` (copied as a regular file) | `~/.factory/commands/<name>.md` |
| subagent | `.factory/droids/<name>.md` | `~/.factory/droids/<name>.md` |
| hook | `.factory/hooks/hooks.json` | `~/.factory/hooks/hooks.json` |
| mcp | `.factory/mcp.json` | `~/.factory/mcp.json` |

Factory commands are materialized as regular files rather than symlinks because the Droid CLI does not follow symlinked command files.

### Gemini CLI

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.gemini/skills/<name>/SKILL.md` | `~/.gemini/skills/<name>/SKILL.md` |
| command | `.gemini/commands/<name>.md` | `~/.gemini/commands/<name>.md` |
| hook | `.gemini/settings.json` | `~/.gemini/settings.json` |
| mcp | `.gemini/gemini-extension.json` | `~/.gemini/gemini-extension.json` |

### Windsurf

| Kind | Repository path | Machine path |
|---|---|---|
| skill | `.windsurf/skills/<name>/SKILL.md` | `~/.codeium/windsurf/skills/<name>/SKILL.md` |
| command | `.windsurf/workflows/<name>` | `~/.codeium/windsurf/global_workflows/<name>` |
| hook | `.windsurf/hooks/<name>` | `~/.codeium/windsurf/hooks/<name>` |
| mcp | `.windsurf/mcp_config.json` | `~/.codeium/windsurf/mcp_config.json` |

Windsurf's repository install root is the repository itself, so the `.windsurf/` prefix is present. The machine install root is already `~/.codeium/windsurf`, so the same files have no prefix.

## Link modes

In the default `symlink` mode, skills and converted commands are links into the registry cache. Factory commands are always copied. Shared aggregate files such as `.codex/hooks.json` and `.devin/mcp_config.json` are reconciled in either mode.

## Related documentation

- [How to configure multiple harnesses](../how-to/configure-multiple-harnesses.md)
- [How to write regular files instead of symlinks](../how-to/write-regular-files-instead-of-symlinks.md)
