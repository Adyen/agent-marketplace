# How to remove or override resources

Remove a selected resource, exclude an auto-selected one, or override a resource from a different configuration layer.

## Prerequisites

- A configured `.agents/config.yaml` or `~/.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

These commands need an editable v2/v3 config. For v4 Git/directory sources, change the selection or exclusion in `resources` and run `sync`, since CLI `remove` cannot edit v4 files. Hosted v4 requests also have lock requirements; see [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

## 1. Remove an explicit selection

Remove a skill that was installed directly:

```bash
agent-marketplace remove code-review --scope repo
```

This deletes the entry from repository config. Without `--scope`, the command chooses a scope from the selection it finds. Specify the scope to avoid removing the resource from the wrong one.

## 2. Exclude an auto-selected resource

If the resource was selected by a source group's `all` or `tags` policy, `remove` records an `exclude` instead:

```bash
agent-marketplace remove legacy-review
```

Expected output:

```text
Excluded legacy-review (provided by example-resources)
```

## 3. `remove` refuses required resources

A `required: true` resource is never converted to an exclusion. `remove` refuses it and reports the source that requires it. Change the organization or user configuration that declares `required: true` if you need to stop materializing it. An organization Marketplace collection without `required: true` is optional: in the TUI, select an individual member and press `e` in the scope modal to add a machine-wide user exclusion to a v4 user config. The collection remains selected and other members remain enabled. The CLI cannot remove a pin from the externally managed organization file. If the user config is still v2/v3, migrate it to v4 before using this TUI override.

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

Under **SYNCED RESOURCES**, check that the resource is gone from the scope you chose. It may still appear in another scope or because a hosted collection selects it. Removing a direct Marketplace selection leaves the collection's selection in place.

## Troubleshooting

- If `remove` reports an ambiguous scope, the same name is enabled in both `user` and `repo` scopes. Add `--scope user` or `--scope repo`.
- If `remove` refuses a required resource, change the config that declares it. Removal and exclusion cannot override `required: true`.
