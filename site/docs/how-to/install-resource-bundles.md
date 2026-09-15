# How to install resource bundles

This guide shows how to install a named collection of resources, or all resources from a registry, in one command.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A configured registry that exposes collections, or a registry whose bundle you want to install.

## 1. List available collections

```bash
agent-marketplace list --kind collection
```

Expected output:

```text
REGISTRY  KIND       NAME                 TAGS  STATUS
example   collection  platform-onboarding        available
```

## 2. Install a collection

Install a collection in repository scope:

```bash
agent-marketplace install platform-onboarding
```

Verify the selection:

```bash
agent-marketplace status
```

Under **SYNCED RESOURCES**, the collection's members appear as separate entries with a `Via` annotation naming the collection.

## 3. Install all resources from a registry

For a one-time ad-hoc install of an entire registry tree:

```bash
agent-marketplace install \
  --from https://github.com/example-org/agent-resources.git \
  --ref v1.4.0 \
  --path resources
```

This writes a registry entry to `.agents/config.yaml` with all discovered resources selected.

## Troubleshooting

- **`platform-onboarding not found`**: The collection is not published in any configured registry. Check the registry `path` and `ref`.
- **Unwanted members are installed**: Remove a member with `agent-marketplace remove <member-name>`. The collection stays selected and the exclusion is recorded in the same scope.
