# How to declare tags and prerequisites

Annotate `SKILL.md` resources with tags and prerequisites so consumers can discover and install them more easily.

## Prerequisites

- A Git registry with `SKILL.md` files.
- `agent-marketplace` on your `PATH`.

## 1. Add metadata tags

Tags are declared under `metadata.tags` in the `SKILL.md` frontmatter:

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

## 2. Add a license

`license` is a top-level frontmatter field:

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

`requires` is under `metadata`:

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

The `kind` field defaults to `skill` when omitted.

## 4. Verify

Install the resource in a consumer checkout:

```bash
agent-marketplace install review-code \
  --from https://github.com/example-org/agent-resources.git \
  --ref v1.4.0
```

The `status` output shows an `unmet requirements` warning if the prerequisite is not also selected.

## Boundaries

- Tags are matched exactly after whitespace is trimmed.
- Prerequisite metadata is advisory. The CLI does not auto-install prerequisites.
- License allowlists are enforced by Marketplace ingestion, not by the consumer CLI.

## Troubleshooting

- **Tags are not honored**: Verify the `tags` field is under `metadata` and is a list of strings.
- **Prerequisite warning persists**: Install the prerequisite explicitly in the same or a higher-precedence scope.
