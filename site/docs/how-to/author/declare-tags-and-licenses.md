# How to declare tags and a license

Add [tags](../../glossary.md#tag) and a license to the front matter of a `SKILL.md`. Tags let consumers install related resources in one step. To name resources a resource needs, see [Declare resource prerequisites](./declare-resource-prerequisites.md).

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

## 3. Declare prerequisites

If the resource needs another resource installed, declare it under `metadata.requires`. See [Declare resource prerequisites](./declare-resource-prerequisites.md) and the [resource prerequisites reference](../../reference/resource-prerequisites.md).

## Boundaries

- Tags match exactly and are case-sensitive. Leading and trailing whitespace is ignored.
- License allowlists are enforced by Marketplace ingestion, not by the consumer CLI.

## Troubleshooting

- Tags have no effect: check that `tags` is under `metadata` and is a list of strings. The CLI ignores items that are not strings.
