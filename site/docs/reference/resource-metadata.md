# Resource metadata reference

Public skill authoring uses YAML frontmatter in `SKILL.md`. The frontmatter is required for the CLI to read the declared `name`, `description`, and `metadata`. Subagent, hook, and MCP files follow their source harness's native schema; see the [registry layout reference](./registry-layout.md).

## Skill `SKILL.md` frontmatter

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

## Top-level fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Resource name. Overrides the directory name. |
| `description` | string | yes | Short description used by search and listings. |
| `license` | string | no | Declared SPDX-like license. |
| `x-agent-marketplace-kind` | string | no | Devin/Codex converted-command marker. Set to `command` inside a skill-shaped `SKILL.md` for those targets; other command formats do not use this field. |
| `metadata` | map | no | Holds `tags` and `requires`. |

## `metadata` fields

| Field | Type | Description |
|---|---|---|
| `metadata.tags` | list of strings | Tags used for tag-based selection. |
| `metadata.requires` | list | Advisory prerequisites. |

## `requires` item

| Field | Type | Default | Description |
|---|---|---|---|
| `kind` | string | `skill` | Resource kind. |
| `name` | string | required | Required resource name. |
| `registry` | string | none | Provider hint. |
| `path` | string | none | Provider path hint. |

Satisfaction is by the `(kind, name)` pair. `registry` and `path` are remediation hints.

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

This marker belongs to Devin/Codex converted-command layouts. Other command formats, such as Claude's `commands/<name>.md`, do not use it.

## Non-skill resources

Subagent, hook, and MCP files use source-harness-specific native formats. The [registry layout reference](./registry-layout.md) lists the supported source paths and link alternatives.

## Related documentation

- [Registry layout reference](./registry-layout.md)
- [How to declare tags and prerequisites](../how-to/author/declare-tags-and-prerequisites.md)
- [How to add a resource to a registry](../how-to/author/add-a-resource-to-a-registry.md)
