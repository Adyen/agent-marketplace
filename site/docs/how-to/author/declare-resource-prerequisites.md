# How to declare resource prerequisites

Tell consumers that a resource needs another one by listing [prerequisites](../../glossary.md#prerequisite) under `metadata.requires` in its front matter. When the prerequisite is not selected, `install`, `sync`, and `doctor` warn with an install command; `status` shows it in a table.

## Prerequisites

- A Git or directory registry with `SKILL.md`, command, or subagent files.
- `agent-marketplace` on your `PATH`.

## 1. Decide the prerequisite kind and name

A prerequisite is a resource of a specific kind — `skill`, `command`, `subagent`, `hook`, or `mcp` — matched by name. A collection cannot be a prerequisite, and a resource that happens to share the name but has a different kind does not count.

## 2. Add the structured item

Put a map under `metadata.requires` when you need more than the name:

```yaml
---
name: review-code
description: Review code with the documentation MCP server available.
metadata:
  requires:
    - kind: mcp
      name: docs-server
      registry: https://github.com/example-org/integrations.git
      path: mcp
---

# review-code

Review code with the documentation MCP server available.
```

`kind` defaults to `skill` when you leave it out and accepts the same spellings as `--kind` (`agents`, `commands`, `mcp_servers`, and so on, matched case-insensitively). `registry` and `path` are optional hints: the CLI copies them into the `--from` and `--path` flags of the install command it suggests. They do not restrict where the prerequisite may come from.

## 3. Use the shorthand for the simple case

A string item is `kind:name`, or a bare name for a skill:

```yaml
metadata:
  requires:
    - mcp:docs-server
    - helper-skill
```

## 4. Verify with `install`

In a consumer project that has your registry configured but does not select `docs-server`, run:

```bash
agent-marketplace install review-code
```

The resource installs anyway. For the sample `registry` and `path` hints above, the output includes:

```text
warning: skill "review-code" requires mcp "docs-server", but it is not selected
  → agent-marketplace install "docs-server" --kind mcp --from "https://github.com/example-org/integrations.git" --path "mcp"
Installed review-code in this repo
```

The warning is advisory: `install`, `sync`, `sync --check`, `status`, and `doctor` exit with status 0 when it is the only finding. `sync --strict` (including `sync --strict --check`) is the exception — it prints the same warning but exits nonzero, so CI can enforce declared prerequisites.

## 5. Verify with `status`

Run `agent-marketplace sync`, then `agent-marketplace status`. Status lists the same prerequisite in an `UNMET REQUIREMENTS` table sorted by `RESOURCE` and then `REQUIRES`:

```text
UNMET REQUIREMENTS
RESOURCE           REQUIRES         FIX
skill:review-code  mcp:docs-server  agent-marketplace install "docs-server" --kind mcp --from "https://github.com/example-org/integrations.git" --path "mcp"
```

## Boundaries

- A prerequisite is met when a resource with that kind and name is selected in the same run, from any source or configuration layer.
- Prerequisites are advisory by default. A consumer who sets `requires.expand: true` in a v4 configuration layer selects declared prerequisites transitively, and `registry`/`path` hints steer that lookup — but only against sources the consumer already configured; the CLI never fetches a new one. See [Requirement expansion](../../reference/resource-prerequisites.md#requirement-expansion).
- URL credentials inside a `registry` hint are replaced with `***` in the suggested command.

## Troubleshooting

- **A prerequisite was skipped.** The CLI skips items without a `name`, with an unknown `kind`, `collection`, or `all`, with non-string fields, or items that are neither a string nor a map. It skips the whole value when `metadata.requires` is not a list. Skipped items are reported by `agent-marketplace list --from <registry>` and `agent-marketplace doctor` as a `catalog.requires.invalid` warning naming the resource, its source file, and the item index; the warning never quotes the rejected value. Fix the indicated item and use a supported kind instead of `all`.
- **The warning does not go away.** Install the prerequisite in any configuration layer that applies to the project, for example with the exact command the warning suggests.

## Related documentation

- [Resource prerequisites reference](../../reference/resource-prerequisites.md)
- [Resource metadata](../../reference/resource-metadata.md)
