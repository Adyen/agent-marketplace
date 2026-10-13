# How to remove or override resources

Remove a resource you selected, turn off one that was selected for you, or override a resource that another configuration layer provides.

## Prerequisites

- A configured `.agents/config.yaml` or `~/.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

`remove` edits v4 source/resource selections. Every existing consumer root, including organization and personal-local, must declare exact v4; fragments may omit the version or declare exact v4. Unsupported input refuses before new acquisition, config/lock publication, or harness changes. [Convert older configurations before upgrading](./migrate-configuration.md). Removing hosted Marketplace resources also involves the owning layer's lock; see [current limits](../explanation/user-journeys.md#before-following-a-journey).

After each change, `remove` syncs, so the resource's files disappear from the harness folders right away.

## 1. Remove an explicit selection

Remove a skill that you installed by name:

```bash
agent-marketplace remove code-review --scope repo
```

The CLI deletes the entry from the repository configuration and prints `Removed code-review`. Without `--scope`, the CLI picks the [scope](../glossary.md#scope) from the layer that selects the resource. Name the scope when you want to be sure which file changes.

## 2. Exclude an auto-selected resource

A resource can be selected without being named, for example by a resource group's `all: true` or `tags` setting, or by a collection. For those, `remove` adds an `exclude` entry instead of deleting anything:

```bash
agent-marketplace remove legacy-review
```

Where the exclusion goes depends on how the resource was selected:

- For a collection member, the CLI adds the exclusion to the file that selects the collection and prints `Excluded legacy-review (provided by collection:<collection-name>)`. The collection stays selected.
- For a repository resource selected by `tags`, the CLI adds the exclusion to `.agents/config.yaml`.
- For a repository resource selected by `all: true`, the CLI adds the exclusion to `.agents/config.local.yaml` when you omit `--scope`. That hides the resource only in your checkout. To hide it for the whole team, run `agent-marketplace remove legacy-review --scope repo`, which writes the exclusion to `.agents/config.yaml`.

For `tags` and `all: true` selections the CLI prints `Removed legacy-review`, even though it only added an exclusion.

## 3. `remove` refuses required resources

A resource from a group with `required: true` ignores every exclusion. `remove` refuses it with an error such as `skill "code-review" is required and cannot be removed`. To stop installing it, change the configuration that declares `required: true`, which is often the organization configuration managed by your administrators.

An organization Marketplace collection without `required: true` is optional. To turn off one of its members on your machine, open the TUI, select the member, and press `e` in the scope modal. The TUI adds a user exclusion to `~/.agents/config.yaml`, which, like every consumer root, must already be v4. [Convert older roots before upgrading](./migrate-configuration.md). The collection stays selected and its other members stay enabled. The CLI cannot remove a pin from the organization file, because that file is managed outside the CLI.

## 4. Target a specific scope

Remove only the user-wide occurrence:

```bash
agent-marketplace remove code-review --scope user
```

Remove only the repository occurrence:

```bash
agent-marketplace remove code-review --scope repo
```

Each prints `Removed code-review` and syncs, so the resource's files disappear from the harness folders right away.

When a resource comes from your user or organization configuration and you omit `--scope`, the CLI writes the change to `~/.agents/config.yaml` and says that it applies across all repositories. You cannot exclude such a resource with `--scope repo` or `--scope local`; the CLI refuses and suggests the scope that works.

## 5. Remove an entire ad-hoc registry

```bash
agent-marketplace remove \
  --from https://github.com/example-org/agent-resources.git \
  --ref v1.4.0
```

If the registry is declared in the target configuration file (the repository file unless you pass `--scope`), the CLI deletes it and prints `Removed registry entry <url>`. If the registry comes from another layer, the CLI adds an entry that excludes all of its resources in the target file and prints `Excluded all resources from <url> (<scope> scope)`. Add `--path` when the same URL is configured more than once.

## 6. Verify the result

```bash
agent-marketplace status
```

Under `SYNCED RESOURCES`, check that the resource is gone from the scope you chose. It can still appear from another scope, or because a hosted collection selects it. Removing a direct Marketplace selection leaves the collection's selection in place.

## Troubleshooting

- If `remove` says the resource `is enabled from multiple config layers`, the same name is selected in more than one scope. The error lists a ready-to-run command for each copy; rerun with the `--scope` (and `--from`, `--path`) of the one you want to remove.
- If `remove` says the name `is ambiguous across kinds`, add `--kind`, for example `--kind command`.
- If `remove` says `resource "<name>" is not currently enabled`, nothing selects that resource right now. Check the name with `agent-marketplace status`.
- If `remove` refuses a required resource, change the configuration that declares `required: true`. Neither removal nor exclusion overrides it.
