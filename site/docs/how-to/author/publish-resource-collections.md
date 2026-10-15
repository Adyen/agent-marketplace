# How to publish resource collections

Define a named [collection](../../glossary.md#collection) in a registry so consumers can install a set of resources in one step.

## Prerequisites

- A Git registry with one or more resources.
- `agent-marketplace` on your `PATH`.

## 1. Create a collection definition

Each collection lives in its own folder under `collections/`, in a file named exactly `COLLECTION.yaml`. Put `collections/` next to the registry's resources, at the root the consumer's source points at (the repository root, or the folder named by the source's `path`).

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

The commands succeed silently and create `collections/platform-onboarding/COLLECTION.yaml`.

Consumers install the collection by its `name`. Members without a `registry` field come from the same registry as the collection. `kind` defaults to `skill` and accepts `skill`, `command`, `subagent`, `hook`, or `mcp`. The CLI skips a flat file such as `collections/platform-onboarding.yaml`.

## 2. Add members from another registry (optional)

A member can name a different registry with `registry`, plus optional `ref` and `path`:

```yaml
resources:
  - kind: command
    name: release
    registry: https://github.com/example-org/shared-commands.git
    path: commands
```

The consumer must already have that registry configured in one of their configuration layers, or the collection fails to expand. Without `ref`, the member uses the ref the consumer configured for that registry.

## 3. Add nested collections (optional)

A collection can include other collections with `includes`:

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

An included collection can come from any registry the consumer has configured.

## 4. Check the collection

From the registry root, run:

```bash
agent-marketplace list --from . --kind collection
```

Expected output:

```text
REGISTRY  KIND        NAME                 TAGS (metadata)  STATUS
.         collection  platform-onboarding                   available
```

If a `COLLECTION.yaml` has no `name`, is not valid YAML, or uses an unsupported member `kind`, the command fails and names the collection or member. `list` does not check that members exist or that their registries are configured; a missing member or unconfigured registry shows up when a consumer syncs or installs the collection.

## 5. Install the collection

In a consumer project:

```bash
agent-marketplace install platform-onboarding \
  --kind collection \
  --from https://github.com/example-org/agent-resources.git \
  --ref v1.4.0
```

This adds `collection/platform-onboarding` to the `include` list of that source in the consumer's configuration, then installs the members. Typical output:

```text
Installed collection platform-onboarding in this repo
```

## Boundaries

- The collection itself is not written into any harness folder. Its members are.
- Nested collections may share children across branches. A collection that includes one of its own ancestors is a cycle, and the CLI rejects it.
- `version` is display-only. The CLI matches collections by `name`, because the source's ref already decides which revision of the registry is used.

## Troubleshooting

- `resource or collection "platform-onboarding" not found in configured registries` (or `not found in registry <registry-url>` when installing with `--from`): check that `collections/` sits at the root of the consumer's source, inside its `path` if it has one.
- A member does not install: sync prints `warning: <kind> "<name>" was selected but not found`, with a note that the collection references it. Check that the member's `kind` and `name` match a real resource and that the consumer has not excluded it.
