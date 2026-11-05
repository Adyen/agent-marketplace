# How to handle a renamed or removed resource

Bring your configuration back in line after a registry renames or deletes a resource you use. To learn why a rename behaves like a removal plus an addition, read [how upstream changes reach consumers](../explanation/how-upstream-changes-reach-consumers.md).

## Prerequisites

- A configuration that selects the resource, such as `.agents/config.yaml`.
- The new name, if the resource was renamed. The registry's changelog or release notes should say. `agent-marketplace list` shows what the locked commit contains.

## 1. Find stale selections

`agent-marketplace update` checks your selections before it moves the lock. When a resource that the locked commit provided no longer exists upstream, `update` refuses and changes nothing:

```text
Error: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
  → run 'agent-marketplace remove --kind skill foo' before rerunning update; install a renamed resource afterwards with 'agent-marketplace install <new-name>'
update left the lock and harness outputs unchanged; rerun 'agent-marketplace update' after resolving the selections above, or update other registries with 'agent-marketplace update --registry <url>'
```

This check is part of the `update` command. A selection can also go stale when the lock moves without it, after `agent-marketplace lock --upgrade` or when the organization deploys a new lock, and when a selection never resolved at the locked commit. Then the resource disappears on the next `sync` and `status` warns:

```bash
agent-marketplace status
```

A stale selection appears under `WARNINGS`:

```text
warning: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
  → this may be caused by an upstream removal, a stale ref pin, a rename, or a cache issue; run 'agent-marketplace status' to diagnose, or 'agent-marketplace remove "foo"' to drop the selection
```

If a collection selects the resource, the hint names the collection instead:

```text
  → this skill is referenced by collection:team; the skill was likely removed or renamed upstream; ...
```

To make stale selections fail a CI job, run `agent-marketplace sync --strict`. It exits `1` with the same message as an `error:` line.

Then follow the section that matches how you selected the resource.

## 2. Fix a selection by name

This covers a group entry such as `include: [skill/foo]`.

When `update` refused, follow its remediation line. Drop the stale entry, then run the update again:

```bash
agent-marketplace remove --kind skill foo
agent-marketplace update
```

Expected output:

```text
Removed foo
Configured registry entries: 1
Marketplace pins updated: 0
```

If the lock already moved without the `update` check, for example after `agent-marketplace lock --upgrade`, `sync --prune-stale` drops the stale entry instead:

```bash
agent-marketplace sync --prune-stale
```

Expected output:

```text
Installed 0  Removed 0  Malformed 0  Errors 0

warning: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
  → this may be caused by an upstream removal, a stale ref pin, a rename, or a cache issue; run 'agent-marketplace status' to diagnose, or 'agent-marketplace remove "foo"' to drop the selection
pruned stale skill "foo" from config
Pruned 1 stale selection(s); re-syncing...
Installed 0  Removed 0  Malformed 0  Errors 0
```

`--prune-stale` removes only direct selections that are not `required`. It skips the whole step, and keeps every selection, when a registry fails to load in that run, because a registry that fails to load makes every resource in it look missing. It cannot be combined with `--locked` or `--offline`. It may also rewrite the quoting of values in the file it edits.

If the resource was renamed, install the new name once the lock has moved:

```bash
agent-marketplace install bar --scope repo
```

Expected output:

```text
Installed bar in this repo
```

The group now reads `include: [skill/keep, skill/bar]`. Add `--scope user` or `--scope local` when the old selection lived in that file.

## 3. Fix an exclude after a rename

This covers groups that use `all: true` or `tags`. The renamed resource installs without any action from you. An `exclude` that named the old name stops matching, so the new name installs even if you excluded the old one, and no warning appears.

To keep the resource hidden, exclude the new name:

```bash
agent-marketplace remove bar --scope repo
```

For an `all: true` group, omitting `--scope` writes the exclude to `.agents/config.local.yaml`, which hides the resource only in your checkout. Delete the old `skill/foo` entry from `exclude` by hand; it matches nothing now.

## 4. Fix a collection member

A collection lists its members by name. You cannot prune a member, because the collection definition lives in the registry.

- If the collection still lists a name the registry no longer has, ask the collection owner to fix the definition. Until then, the warning is harmless, but `sync --strict` fails. `--prune-stale` leaves it in place.
- If you excluded a member that the registry renamed, exclude the new name: `agent-marketplace remove bar`. The CLI adds the exclude to the file that selects the collection and prints `Excluded bar (provided by collection:team)`.
- If the registry renamed or deleted the collection itself, `update` refuses and names the collection, while the members stay installed:

  ```text
  Error: collection "team" was selected but not found in https://github.com/example-org/agent-resources.git@main
    → run 'agent-marketplace remove --kind collection team' before rerunning update; install a renamed resource afterwards with 'agent-marketplace install <new-name>'
  update left the lock and harness outputs unchanged; rerun 'agent-marketplace update' after resolving the selections above, or update other registries with 'agent-marketplace update --registry <url>'
  ```

  Run `agent-marketplace remove --kind collection team`, rerun `update`, then replace `collection/team` with the new collection name in the file that selects it and run `agent-marketplace sync`. While the lock still holds the old commit, plain `sync` keeps installing the members.

## 5. Fix a required selection

A `required: true` group that names a missing resource makes `update` refuse with a different remediation line:

```text
Error: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
  → required selection: 'agent-marketplace remove' cannot drop it; change the configuration entry that marks it required, or ask that configuration's owner
update left the lock and harness outputs unchanged; rerun 'agent-marketplace update' after resolving the selections above, or update other registries with 'agent-marketplace update --registry <url>'
```

The lock has not moved, and `sync` still passes. `remove` refuses required resources, so edit the configuration file by hand. Replace the old name in the required group, for example `include: [skill/bar]`, and run `agent-marketplace update` again.

A required miss still fails `sync` when the lock moved without the `update` check, most often when an organization deploys its upgraded lock:

```text
Installed 0  Removed 0  Malformed 0  Errors 1

Error: error: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
  → this may be caused by an upstream removal, a stale ref pin, a rename, or a cache issue; run 'agent-marketplace status' to diagnose, or 'agent-marketplace remove "foo"' to drop the selection
```

If the required group lives in the organization file, ask its administrator to change it. You cannot override a required selection from another file.

## 6. Replace a hosted Marketplace pin

A pinned Marketplace revision keeps installing after a rename. At `update`, the Marketplace answers that the resource is gone, so `update` keeps that pin at its current revision, updates your other pins, and exits `1` with an error that names each missing resource:

```text
Configured registry entries: 0
Marketplace pins updated: 0
Other Marketplace pins skipped: 1
Marketplace "https://marketplace.example.com" "shared-agents"/"skill"/"foo": skipped ("resource no longer exists on the Marketplace")
Error: 1 Marketplace resource(s) no longer exist:
marketplace https://marketplace.example.com pin shared-agents/skill/foo: resource not found; remove the old pin with `agent-marketplace remove foo` and install the new name
```

Remove the old pin and install the new name from the same scope:

```bash
agent-marketplace remove foo --scope repo
agent-marketplace install marketplace:shared-agents/skill/bar --scope repo
```

Expected output:

```text
Removed foo
Installing bar · skill

Source: shared-agents

✓ Installed bar
```

Replace `marketplace` with the alias of your Marketplace source, and `shared-agents` with its registry. Then run `agent-marketplace update` again to move your other pins.

## 7. Commit the result

For repository configuration, commit the configuration and the lock together:

```bash
git add .agents/config.yaml .agents/lock.yaml
git commit -m "Follow upstream rename of foo to bar"
```

Teammates pick up the change with a plain `agent-marketplace sync` after they pull.

## Troubleshooting

- **`prune-stale skipped: the catalog could not be trusted this run`**: a registry failed to load. Fix the errors that the same run printed, then run `agent-marketplace sync --prune-stale` again.
- **The warning appears for a name you just added**: the resource exists upstream but not at the locked commit. Run `agent-marketplace update` to move the lock forward.
- **An organization resource disappeared and nothing in your files mentions it**: the organization changed its configuration or upgraded its lock. `agent-marketplace status` shows `SOURCE ORG` for organization resources. Ask the administrator.

## Related documentation

- [How upstream changes reach consumers](../explanation/how-upstream-changes-reach-consumers.md)
- [How to remove or override resources](./remove-or-override-resources.md)
- [How to review and roll back an update](./review-and-roll-back-updates.md)
- [`sync` reference](../reference/cli.md#sync)
