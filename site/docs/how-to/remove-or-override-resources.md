# How to remove or override resources

This guide shows how to remove a selected resource, exclude an auto-selected one, or override a resource from a different configuration layer.

## Prerequisites

- A configured `.agents/config.yaml` or `~/.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

## 1. Remove an explicit selection

Remove a skill that was installed directly:

```bash
agent-marketplace remove code-review
```

This deletes the entry from the selected config scope. The default scope is `repo`.

## 2. Exclude an auto-selected or default resource

If the resource was selected because the registry has `default: true` or because it was auto-selected, `remove` records an `exclude` instead:

```bash
agent-marketplace remove legacy-review
```

Expected output:

```text
Excluded legacy-review (provided by example-resources)
```

## 3. `remove` refuses required resources

A `required: true` resource is never converted to an exclusion. `remove` refuses it and reports the source that requires it. Change the organization or user configuration that declares `required: true` if you need to stop materializing it.

## 4. Target a specific scope

Remove only the user-wide occurrence:

```bash
agent-marketplace remove code-review --scope user
```

Remove only the repository occurrence:

```bash
agent-marketplace remove code-review --scope repo
```

## 5. Remove an entire ad-hoc registry

```bash
agent-marketplace remove \
  --from https://github.com/example-org/agent-resources.git \
  --ref v1.4.0
```

This removes or shadows the registry entry in the selected scope.

## 6. Verify the result

```bash
agent-marketplace status
```

Confirm the resource no longer appears under **SYNCED RESOURCES**.

## Troubleshooting

- **`remove` reports an ambiguous scope**: The same name is enabled in both `user` and `repo` scopes. Add `--scope user` or `--scope repo`.
- **`remove` refuses a required resource**: `required: true` resources are immune to removal and exclusion. Change the configuration that declares them.
