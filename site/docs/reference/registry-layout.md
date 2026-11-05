# Registry layout reference

This page lists the files and folders `agent-marketplace` reads in a registry, and how it decides which harness layout a registry uses. Registry authors need it to place resources where the CLI discovers them. Consumers need it when a resource they expect is missing.

## Layout by kind

| Kind | Directory | Expected files |
|---|---|---|
| skill | `skills/<name>/` in most layouts; see the table below | `SKILL.md` with top-level `name`, `description`, and optional `license`; `metadata` holds `tags` and [`requires`](./resource-prerequisites.md). Other files in the folder ship with the skill. |
| command | source-harness-specific, for example `commands/<name>.md` for Claude or `skills/<name>/` with `x-agent-marketplace-kind: command` for Devin and Codex | Native command file, or a skill-shaped command for the source harness. |
| subagent | source-harness-specific; see the table below | Native subagent file for the source harness. |
| hook | source-harness-specific; see the table below | Native hook file or shared settings file for the source harness. |
| mcp | source-harness-specific; see the table below | Native MCP file or shared settings file for the source harness. |
| collection | `collections/` | `collections/<name>/COLLECTION.yaml`. See the [collections reference](./collections.md). |

There are no generic `subagents/` or `mcp/` discovery roots. The CLI reads each source harness's native layout, so a subagent named `review` ships as `agents/review.md` in a Claude registry, not `subagents/review.md`, and MCP servers ship in a root-level `.mcp.json` in a Claude registry, not `mcp/mcp.json`. Files in unrecognized directories are not discovered.

### Source paths by harness

Paths are relative to the registry root, or to the source's `path` when it has one.

| Harness | Skill | Command | Subagent | Hook | MCP server |
|---|---|---|---|---|---|
| Claude Code | `skills/<name>/SKILL.md` | `commands/<name>.md` | `agents/<name>.md` | `hooks/hooks.json` | `.mcp.json` at the bundle root; without `.claude-plugin/plugin.json`, also `mcp.json` when `.mcp.json` is absent |
| Factory Droid | `skills/<name>/SKILL.md` | `commands/<name>.md` | `droids/<name>.md` | `hooks/hooks.json` | `mcp.json` at the bundle root |
| Cursor | `skills/<name>/SKILL.md` | `commands/<name>.md` | `agents/<name>.md` | `hooks/hooks.json` | `mcp.json` at the bundle root |
| Windsurf | `skills/<name>/SKILL.md` | `workflows/<name>.md` | not supported | `hooks.json`, or one file per hook at `hooks/<name>.json` when `hooks.json` is absent | `mcp_config.json` |
| Gemini CLI | `.agents/skills/<name>/SKILL.md` or `.gemini/skills/<name>/SKILL.md` | `commands/<name>.toml` | `.gemini/agents/<name>.md` | `.gemini/settings.json` under `hooks` | `.gemini/settings.json` under `mcpServers`; otherwise `gemini-extension.json`, `.gemini/gemini-extension.json`, or `.gemini/mcp.json` |
| Devin CLI | `skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` with `x-agent-marketplace-kind: command` | `agents/<name>.md` or `agents/<name>/AGENT.md` | `hooks.v1.json` (project layout) or `config.json` under `hooks` | `mcp_config.json` |
| Codex | `.agents/skills/<name>/SKILL.md` | `.agents/skills/<name>/SKILL.md` with `x-agent-marketplace-kind: command` | `.codex/agents/<name>.toml` | `.codex/hooks.json` | `.codex/config.toml` under `mcp_servers` |
| OpenCode | `.opencode/skills/<name>/SKILL.md` | `.opencode/commands/<name>.md` | `.opencode/agents/<name>.md` with `mode: subagent` | `.opencode/hooks/agent-marketplace/<name>.json` beside the fixed adapter in `.opencode/plugins/` | `opencode.json` or `opencode.jsonc` under `mcp` |

In the Claude, Factory, Cursor, Windsurf, Devin, and Codex layouts, a `SKILL.md` without a `name` takes its folder name. The Claude `mcp.json` fallback applies to current lock source revisions; a Git source locked at source revision v1 to v3 keeps reading only `.mcp.json` until its lock is upgraded (see the [lockfile reference](./lockfiles.md)).

Workspace layouts under each harness folder are also recognized: `.claude/`, `.cursor/`, `.factory/`, `.windsurf/`, and `.devin/` prefix the same relative paths (for example `.claude/agents/<name>.md`). Two MCP locations differ in workspace form: a Claude workspace reads `.mcp.json` or `.claude.json` at the root before `.claude/.mcp.json`, and a Windsurf workspace reads `.windsurf/mcp_config.json` before a root-level `mcp_config.json`. Codex sources always use the `.agents/` and `.codex/` partitions shown above. Codex also reads legacy prompts from `.codex/prompts/<name>.md` as commands and reports `ingest.codex.prompt-deprecated` for each.

## Source harness

The CLI detects the layout from marker files, checked in this order:

1. A `.devin/` folder with Devin skills, agents, or settings files: Devin.
2. A plugin manifest: `.claude-plugin/plugin.json` (Claude), `.factory-plugin/plugin.json` (Factory), `.cursor-plugin/plugin.json` (Cursor), or `gemini-extension.json` (Gemini).
3. A `.codex/` folder: Codex.
4. A `.claude/` workspace; root-level `skills/`, `commands/`, `agents/`, `hooks/`, or `.mcp.json` without a `droids/` folder; a folder of skill subfolders; or a `SKILL.md` at the root: Claude.
5. A `.factory/` workspace, or a root-level Factory layout that step 4 did not claim, for example one with a `droids/` folder: Factory.
6. A `.cursor/` workspace: Cursor.
7. Gemini files under `.gemini/` (skills, agents, `settings.json`, `mcp.json`, or `gemini-extension.json`), or a non-empty `.agents/skills/`: Gemini.
8. A `.windsurf/` workspace, or root-level `workflows/`, `hooks.json`, `mcp_config.json`, or `AGENTS.md`: Windsurf.

An OpenCode marker is an `.opencode/` folder holding `skills/`, `commands/`, `agents/`, `plugins/` (or their singular forms), `hooks/agent-marketplace/`, or an `opencode.json`/`opencode.jsonc` file. Dependency files OpenCode writes there by itself, such as `package.json`, are not a marker. The marker is weighed against steps 3-8: when the layout they select has its own workspace folder (`.codex/`, `.claude/`, `.factory/`, `.cursor/`, `.gemini/`, or `.windsurf/`), that layout keeps precedence; otherwise OpenCode wins, including over `.agents/skills/` and bare skill folders. A root whose only layout is the OpenCode marker but that contains bundle folders stays a container of those bundles. Whenever both are present, the catalog reports the warning `ingest.autodetect.ambiguous`, because the other layout's resources are not read. Set `source_harness` to choose explicitly.

When nothing matches, the CLI reads the registry as Claude and records the diagnostic `catalog.autodetect.fallback`.

Root-level Cursor, Windsurf, Factory, and Gemini (`commands/<name>.toml`) layouts share folder names with Claude, and a root-level Devin layout (without `.devin/`) is never detected. For those registries, set `source_harness` on the source declaration:

```yaml
apiVersion: agent-marketplace/v4
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    source_harness: claude
resources:
  shared:
    all: true
```

## Registry root shapes

A registry root, or the folder a source's `path` points at, can take one of these shapes:

- One bundle in any layout above.
- A single skill: a `SKILL.md` directly at the root. Its front matter must declare `name`, because there is no folder name to fall back on.
- A folder of skills: subfolders that each contain a `SKILL.md`, for example when `path: skills` points at a `skills/` folder directly.
- A container of several bundles. When the root has no resources of its own, the CLI searches its subfolders for bundles and reads each one separately. It does not search `.git`, `.github`, `node_modules`, `.cache`, `test`, `tests`, `__tests__`, harness folders such as `.claude/`, or resource folders such as `skills/`. Each bundle keeps its own `collections/` folder.

When a root has its own resources and also contains subfolders that look like separate bundles, the CLI reads only the root and reports `catalog.discover.sub-bundles-ignored`. Move the root resources into a subfolder such as `common/` so the root becomes a pure container.

## Ingestion roots

A Git source can limit discovery to one subdirectory with `path`. For example, `path: resources` discovers only `resources/skills/...`, `resources/commands/...`, and so on. A `directory` source points at the folder to read directly and has no `path`.

When operating a hosted Marketplace, `roots` declare ingestion paths relative to the registry origin.

## Conversion direction

Every supported harness can be a conversion source and a conversion destination.

## Related documentation

- [How to add a resource to a registry](../how-to/author/add-a-resource-to-a-registry.md)
- [Resource metadata reference](./resource-metadata.md)
- [Collections reference](./collections.md)
