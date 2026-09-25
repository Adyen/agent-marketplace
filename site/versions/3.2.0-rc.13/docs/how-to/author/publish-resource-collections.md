# How to publish resource collections

Define a named collection in a registry so consumers can install a bundle of resources at once.

## Prerequisites

- A Git registry with one or more resources.
- `agent-marketplace` on your `PATH`.

## 1. Create a collection definition

Collections are defined in `collections/<name>/COLLECTION.yaml`. Create one collection per directory:

```bash
mkdir -p collections/platform-onboarding
cat > collections/platform-onboarding/COLLECTION.yaml << 'EOF'
name: platform-onboarding
description: Common starter resources for platform teams
resources:
  - kind: skill
    name: frontend-design
  - kind: command
    name: release
  - kind: mcp
    name: docs-server
EOF
```

## 2. Add nested collections (optional)

A collection can include other collections with the `includes` key:

```yaml
name: onboarding
includes:
  - name: security
  - name: platform
    version: v1.2.0
resources:
  - kind: skill
    name: welcome
```

## 3. Validate the collection

From the registry root, run:

```bash
agent-marketplace list --from . --kind collection
```

Expected output:

```text
REGISTRY  KIND        NAME                 TAGS  STATUS
.         collection  platform-onboarding        available
```

## 4. Install the collection

In a consumer checkout:

```bash
agent-marketplace install platform-onboarding \
  --from https://github.com/example-org/agent-resources.git \
  --ref v1.4.0
```

This writes a `collections` selector in the consumer's configuration.

## Boundaries

- Collections are catalog-selectable bundles. They are not materialized themselves; their members are.
- Nested collections form a DAG. An ancestor repeated on the same include path is a cycle.
- The `version` field is display-only metadata; matching is by `name` because the registry ref already pins which revision is in use.

## Troubleshooting

- **`platform-onboarding not found`**: Confirm the registry path includes the `collections/` directory.
- **A member does not install**: Check that the member's `kind` and `name` match a real resource and that it is not excluded.
