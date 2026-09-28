# How to declare tags and prerequisites

Add [tags](../../glossary.md#tag), a license, and prerequisites to the front matter of a `SKILL.md`. Tags let consumers install related resources in one step, and prerequisites warn consumers when a resource needs another one they have not installed.

## Prerequisites

- A Git registry with `SKILL.md` files.
- `agent-marketplace` on your `PATH`.

## 1. Add tags

Put tags under `metadata.tags` as a list of strings:

```yaml
---
name: secure-inputs
description: Validate and sanitize user input.
metadata:
  tags:
    - security
    - frontend
---

# secure-inputs

Validate and sanitize user input.
```

Consumers select every resource with a tag through `tags:` in a resource group. Commands and subagents written as Markdown with front matter take `metadata.tags` in the same place. `agent-marketplace list --from .` shows the tags in the `TAGS (metadata)` column.

## 2. Add a license

`license` is a top-level front matter field, next to `name`:

```yaml
---
name: secure-inputs
description: Validate and sanitize user input.
license: Apache-2.0
metadata:
  tags:
    - security
---
```

## 3. Declare a prerequisite

Put prerequisites under `metadata.requires`:

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

`kind` defaults to `skill` when you leave it out. `registry` and `path` are optional. The CLI uses them to build the install command it suggests to consumers.

A prerequisite can also be a single string: `mcp:docs-server` for a resource of another kind, or just `other-skill` for a skill.

## 4. Check the result

Install the resource in a consumer project:

```bash
agent-marketplace install review-code \
  --from https://github.com/example-org/agent-resources.git \
  --ref v1.4.0
```

If `docs-server` is not selected, the command installs `review-code` and prints a warning with the command that would install the prerequisite:

```text
warning: skill "review-code" requires mcp "docs-server", but it is not selected
  → agent-marketplace install "docs-server" --kind mcp --from "https://github.com/example-org/integrations.git" --path "mcp"
Installed review-code in this repo
```

`sync`, `sync --check`, and `doctor` repeat the warning, and `agent-marketplace status` lists it in an `UNMET REQUIREMENTS` table with `RESOURCE`, `REQUIRES`, and `FIX` columns.

## Boundaries

- Tags match exactly and are case-sensitive. Leading and trailing whitespace is ignored.
- Prerequisites are advisory. The CLI never installs them automatically, and the requiring resource is installed anyway.
- A prerequisite is met when a resource with that kind and name is selected for the same run, from any source or configuration layer. `registry` and `path` do not restrict where it comes from.
- `kind` accepts `skill`, `command`, `subagent`, `hook`, or `mcp`. A collection cannot be a prerequisite. The CLI skips an entry with an unknown kind or without a `name` and records a warning in its log file, not in the command output.
- License allowlists are enforced by Marketplace ingestion, not by the consumer CLI.

## Troubleshooting

- Tags have no effect: check that `tags` is under `metadata` and is a list of strings. The CLI ignores items that are not strings.
- The prerequisite warning does not go away: install the prerequisite in any configuration layer that applies to the project, for example with the command the warning suggests.
