# Native harness materialization

`agent-marketplace` does not impose a single output format. Instead, it compiles the same canonical resource into the native files each harness expects.

## Why native layouts

Coding agents read their own formats. Claude Code looks for `.claude/skills/<name>/SKILL.md`, Factory Droid for `.factory/skills/<name>/SKILL.md`, Codex for `.agents/skills/<name>/SKILL.md` plus `.codex/` for everything else. Writing native output means the generated files work the moment the harness starts, with no adapter in between. The [output paths reference](../reference/harnesses-and-output-paths.md) has the full matrix.

## Where compilation happens

The pipeline that turns a registry resource into native files is described step by step in [How agent-marketplace works](./how-agent-marketplace-works.md). This page covers what happens at the last step: how the compiled artifacts land on disk.

## Honest about loss

When a target cannot represent a field or value, the compiler emits a stable diagnostic and may skip that part of the resource. It never silently invents data.

## Link versus file

In the default `symlink` mode, the CLI creates links to the compiled cache for skills and converted command directories. In `file` mode, the CLI writes regular files and trees. Shared configuration files such as `.codex/hooks.json` and `.devin/mcp_config.json` are always ownership-reconciled, not linked.

## Ownership and shared aggregates

Devin and Codex store durable ownership in XDG state so the CLI can distinguish generated entries from hand-authored ones. This lets `sync` preserve user edits while still updating managed entries.

## Related documentation

- [Harnesses and output paths reference](../reference/harnesses-and-output-paths.md)
- [How to convert a harness layout](../how-to/convert-a-harness-layout.md)
- [How agent-marketplace works](./how-agent-marketplace-works.md)
