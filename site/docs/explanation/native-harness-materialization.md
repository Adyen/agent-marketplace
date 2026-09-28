# Native harness materialization

Each harness reads resources from its own folders, in its own file format. `agent-marketplace` converts every resource into that native format and writes it where the harness expects it. The product calls this last step [materialize](../glossary.md#materialize). This page explains what ends up on disk and why.

## Why native layouts

Claude Code looks for skills in `.claude/skills/<name>/SKILL.md`, and Factory Droid in `.factory/skills/<name>/SKILL.md`. Codex reads skills from `.agents/skills/<name>/SKILL.md` and everything else from `.codex/`. Because the CLI writes each harness's own layout, the harness finds the files the next time it starts, with no plugin or adapter in between. The [output paths reference](../reference/harnesses-and-output-paths.md) lists every path for every harness.

## Where conversion happens

[How agent-marketplace works](./how-agent-marketplace-works.md) walks through the whole pipeline, from reading configuration to converting each resource. This page covers only the last step: how the converted files land on disk.

## What happens when a harness cannot represent something

Some harnesses lack a feature that another one has. Windsurf, for example, has no subagents. When a target cannot represent a field, a value, or a whole resource, the CLI reports a [diagnostic](../glossary.md#diagnostic) with a fixed code and may leave that part out. It never makes up data to fill the gap.

## Links or regular files

In the default `symlink` mode, each installed skill, command, and subagent is a [symlink](../glossary.md#symlink) that points to a converted copy in the cache under `~/.cache/agent-marketplace/`. Factory commands are the one exception: they are written as regular files, with a `_managed_by: agent-marketplace` line in their front matter so the CLI can recognize them later.

In `file` mode, set through `materialization.mode: file`, the CLI writes regular files and folders instead, and keeps a manifest of what it wrote. [Write regular files instead of symlinks](../how-to/write-regular-files-instead-of-symlinks.md) shows how to switch.

The CLI removes a file it installed earlier only when it can tell that it wrote the file: a symlink into its cache, a marked Factory command, or a file-mode copy listed in the manifest whose content has not changed. Any other file in these folders belongs to you, and `sync` leaves it alone.

## Shared configuration files

MCP servers and hooks do not get one file each. The harness keeps them as entries in one shared file, such as `.mcp.json`, `.gemini/settings.json`, or `.codex/hooks.json`, which can also hold entries you wrote yourself. The CLI never replaces such a file with a link. It edits only the entries it manages and keeps everything else in the file. The way it recognizes its own entries depends on the harness:

- For Claude, Factory, Cursor, and Windsurf, each managed MCP server or hook entry carries a `"_managed_by": "agent-marketplace"` field. Hooks in Gemini's `.gemini/settings.json` are marked the same way.
- For Gemini MCP servers, and for every shared file of Devin and Codex, the CLI writes no marker. It records which entries it wrote in separate ownership state on your machine. Gemini rejects unknown fields in MCP entries, and a marker in a file is something any tool or person could copy. An old Gemini MCP entry that carries a marker but has no ownership record is kept and reported, never taken over.

[Shared configuration ownership](./shared-configuration-ownership.md) explains the ownership state and what happens when it is missing.

## Related documentation

- [Harnesses and output paths reference](../reference/harnesses-and-output-paths.md)
- [How to convert a harness layout](../how-to/convert-a-harness-layout.md)
- [How agent-marketplace works](./how-agent-marketplace-works.md)
