# Resource metadata reference

This page lists the front matter fields `agent-marketplace` reads from a `SKILL.md`: the YAML block between two `---` lines at the top of the file. Registry authors use it to name, describe, tag, and license skills. Subagent, hook, and MCP files follow their source harness's native schema; see the [registry layout reference](./registry-layout.md).

## Skill `SKILL.md` front matter

```yaml
---
name: code-review
description: Review code changes and suggest improvements.
license: Apache-2.0
metadata:
  tags:
    - review
    - quality
  requires:
    - kind: mcp
      name: docs-server
---

# code-review

Review code changes and suggest improvements.
```

The CLI also loads a `SKILL.md` without front matter. It then uses the folder name as the name and leaves the description empty. The [Agent Skills specification](https://agentskills.io/specification) requires both `name` and `description`, so always include them.

A `SKILL.md` whose front matter is not valid YAML, or whose name breaks the naming rule below, is reported as malformed with a diagnostic such as `ingest.claude.load-skill-failed` and is not installed.

## Top-level fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | Required by the specification. The CLI falls back to the folder name, except for a `SKILL.md` at the registry root, which must declare it. | Resource name. Takes precedence over the folder name. Must start with a lowercase letter and contain only lowercase letters, digits, and hyphens. |
| `description` | string | Required by the specification. The CLI accepts an empty value. | Short description shown with the resource. |
| `license` | string | no | Declared license, usually an SPDX identifier such as `Apache-2.0`. The consumer CLI carries it along unchanged. A hosted Marketplace reads it (or `metadata.license`) for its license policy and falls back to the license configured for the registry. |
| `x-agent-marketplace-kind` | string | no | Devin and Codex converted-command marker. Set to `command` inside a skill-shaped `SKILL.md` in those layouts; other command formats do not use this field. |
| `metadata` | map | no | Holds `tags` and `requires`. |

Harness-specific skill fields such as `allowed-tools`, `model`, and `argument-hint` are also read and translated for each target harness where it has an equivalent. The [field matrix](https://github.com/Adyen/agent-marketplace/blob/main/pkg/harness-compilation/field-matrix.md) lists them. Other unknown fields are kept and passed through.

## `metadata` fields

| Field | Type | Description |
|---|---|---|
| `metadata.tags` | list of strings | Tags used for tag-based selection. Matching is exact and case-sensitive after surrounding whitespace is trimmed. Items that are not strings are ignored. |
| `metadata.requires` | list | Advisory prerequisites. Each item is a map (below) or a string. |

Commands and subagents written as Markdown with front matter read `metadata.tags` and `metadata.requires` from the same place.

## `requires` item

Each `metadata.requires` item is a map with `name` and optional `kind`, `registry`, and `path`, or a string shorthand (`mcp:docs-server`, or a bare name for a skill). An unmet prerequisite produces an advisory warning; only `sync --strict` (including `sync --strict --check`) fails on it. The full declaration grammar, satisfaction rule, warning format, and exit status live in the [resource prerequisites reference](./resource-prerequisites.md).

## Command marker

A Devin or Codex converted command is a skill-shaped file that carries the top-level marker:

```yaml
---
name: release
description: Cut a release
x-agent-marketplace-kind: command
---

# release

Run the release checklist.
```

This marker belongs to Devin and Codex converted-command layouts. Other command formats, such as Claude's `commands/<name>.md`, do not use it.

A marked command cannot also carry controls a command cannot hold. In a Devin layout, `permissions`, `agent`, `subagent`, or `triggers` that do not include `user` make the CLI refuse the resource with `ingest.devin.command-security-unsupported`. Remove the marker to keep such a file as a skill.

## Non-skill resources

Subagent, hook, and MCP files use source-harness-specific native formats. The [registry layout reference](./registry-layout.md) lists the supported source paths.

## Related documentation

- [Registry layout reference](./registry-layout.md)
- [Resource prerequisites reference](./resource-prerequisites.md)
- [How to declare resource prerequisites](../how-to/author/declare-resource-prerequisites.md)
- [How to declare tags and a license](../how-to/author/declare-tags-and-licenses.md)
- [How to add a resource to a registry](../how-to/author/add-a-resource-to-a-registry.md)
