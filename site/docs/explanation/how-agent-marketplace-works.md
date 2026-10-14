# How agent-marketplace works

`agent-marketplace` installs shared AI agent [resources](../glossary.md#resource), such as skills, commands, and MCP servers, into the coding tools you use. The docs call each of those tools a [harness](../glossary.md#harness): Claude Code, Cursor, Codex, and so on. You list the resources you want in a configuration file. The CLI finds them in a [registry](../glossary.md#registry) (a Git repository or a folder), pins each Git registry to one exact [commit](../glossary.md#commit), converts every resource into the format each harness reads, and writes the result into the folders where that harness looks.

This page follows one `sync` from start to finish, then explains the design choices behind it.

## A worked example

Suppose `.agents/config.yaml` in your project selects the `frontend-design` skill from Anthropic's public skills repository, and lists Claude Code and Cursor as targets. Running `agent-marketplace sync` does the following:

1. It loads the configuration. Up to four files combine, from lowest to highest priority: organization (`/etc/agent-marketplace/config.yaml`), user (`~/.agents/config.yaml`), repository (`.agents/config.yaml`), and personal-local (`.agents/config.local.yaml`). A higher file overrides a lower one. The [configuration layers reference](../reference/configuration-layers.md) gives the exact rules.
2. It checks the [lockfile](../glossary.md#lockfile). The source's `ref: main` names a branch, and a branch keeps moving as people push to it. On the first run, `sync` looks up the commit `main` points to right now and records it in `.agents/lock.yaml`, together with a digest (a fingerprint) of every resource under the source. On later runs it reuses that recorded commit, even after `main` has moved on.
3. It downloads that exact commit into the [cache](../glossary.md#cache) at `~/.cache/agent-marketplace/` and checks the files against the digests in the lock. If they differ, it stops.
4. It finds the resources. The CLI searches the downloaded files for the folder layout a harness uses, here Claude's `skills/<name>/SKILL.md`. Your selections, tags, collections, and excludes, plus the priority between configuration files, then decide that `frontend-design` is the one skill to install.
5. It reads each selected resource into a harness-neutral description. `SKILL.md` becomes, in memory, "a skill named `frontend-design` with this description, this body, and these support files", with no trace of which harness's format it came from. Commands, subagents, hooks, and MCP servers each have their own description of this kind.
6. It converts that description for each target. The skill becomes Claude Code's `.claude/skills/frontend-design/SKILL.md` and Cursor's `.cursor/skills/frontend-design/SKILL.md`. When a target has no way to express something (a field, a hook event, or a whole resource kind), the CLI reports a [diagnostic](../glossary.md#diagnostic) with a fixed code and leaves that part out. It does not guess.
7. It writes the files into the harness folders. The product calls this step [materialize](../glossary.md#materialize). By default each skill, command, and subagent is a [symlink](../glossary.md#symlink) (a shortcut) to a converted copy in the cache; Factory commands are the exception and are written as regular files. Shared configuration files such as `.mcp.json` hold entries from several places, so the CLI edits only its own entries in them. Output that the CLI generated earlier, and that the configuration no longer selects, is removed.

`install` edits a configuration file and then runs the same steps. `sync --check` runs them without writing anything and reports what is out of date.

## How resources are found

A registry can be written in any harness's layout. The CLI recognizes each layout by its folder and file names. For example, a Claude registry keeps subagents in `agents/<name>.md` and MCP servers in a `.mcp.json` at its root, while a Codex registry uses `.codex/agents/<name>.toml` and `.codex/config.toml`. A source's `path:` limits the search to one subfolder. Its `source_harness:` names the layout when the CLI cannot tell on its own. Collections (`collections/<name>/COLLECTION.yaml`) are read in the same pass and replaced by their member resources before selection. The [registry layout reference](../reference/registry-layout.md) lists every recognized path.

## Why resources pass through a neutral description

The CLI supports seven harnesses. Converting directly from each one to each other one would take 42 separate converters. With a harness-neutral description in the middle, each harness needs only two pieces: one that reads its format into the description, and one that writes the description back out in its format. Any harness can then be a source or a target.

The conversion keeps meaning, not exact bytes. A Claude skill converted back to Claude comes out unchanged. Some conversions lose information because the target has no equivalent: Windsurf has no subagents, and Codex hooks support only some events. The CLI reports every such loss as a diagnostic (codes such as `compile.*`, `skill.*`, and `hook.*`) instead of dropping it silently. See the [diagnostics reference](../reference/diagnostics.md).

## Where state lives

| State | Location | Committed to Git? | Safe to delete? |
|---|---|---|---|
| Repository selections | `.agents/config.yaml` | yes | no |
| Repository lock | `.agents/lock.yaml` | yes | a writing `sync` recreates it, but it records whatever commit the ref points to at that moment, so you lose the old pin |
| Personal selections | `~/.agents/config.yaml`, `~/.agents/lock.yaml` | no | no |
| Personal overrides for one checkout | `.agents/config.local.yaml`, `.agents/lock.local.yaml` | no; when `.agents/config.local.yaml` exists, the CLI adds both to `.gitignore` | the lock yes; deleting the config removes your personal overrides |
| Cache | `~/.cache/agent-marketplace/` | no | yes; installed symlinks point into it and stay broken until the next `sync` downloads the locked commits again, which needs network access |
| Ownership state for Devin, Codex, and Gemini MCP entries | `$XDG_STATE_HOME/agent-marketplace/` | no | no; without it, every existing entry in those shared files counts as handwritten |
| Generated harness files | Symlinks and generated files inside `.claude/`, `.cursor/`, `~/.claude/`, and similar folders | usually not; the CLI does not add them to `.gitignore` | yes; `sync` recreates them from the configuration and lock |

The configuration files and lockfiles are the record of what you asked for. `sync` can rebuild the cache and the generated harness files from them. Do not delete ownership state or files you wrote by hand. Without ownership state, the CLI treats every existing entry in the files it tracks as handwritten: it keeps them all, refuses to write an entry whose name is already taken, and removes nothing until you repair the state. Your configuration cannot recreate handwritten settings or the safety backups stored next to the ownership state. [Shared configuration ownership](./shared-configuration-ownership.md) explains this in more detail.

## Related documentation

- [Sources and resources](./sources-and-resources.md)
- [Scopes, selection, and precedence](./scopes-selection-and-precedence.md)
- [Reproducibility and lockfiles](./reproducibility-and-lockfiles.md)
- [Native harness materialization](./native-harness-materialization.md)
- [Harnesses and output paths reference](../reference/harnesses-and-output-paths.md)
