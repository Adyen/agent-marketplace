# How upstream changes reach consumers

Registries change after you install from them. Authors add skills, delete old ones, rename them, and edit collections. This page explains when such a change reaches a machine, what it does to your selections, and what happens to the files in your harness folders. With this model you can predict the result of the next `sync` or `update` before you run it.

The answer depends on three things:

1. Which configuration file declares the source, and whether that file has a lock. This decides **when** the change arrives.
2. How the configuration selects the resource: by name, by `all: true` or `tags`, through a collection, or with `required: true`. This decides **what** the change does.
3. Whether the CLI can prove it wrote the files in the harness folder. This decides **which files** it removes or replaces.

## When a change arrives

A [lockfile](../glossary.md#lockfile) records the exact commit or Marketplace revision that each source resolved to. While a lock holds, a push to the registry changes nothing on your machine. A plain `sync` keeps installing what the lock records. The change arrives only when someone moves the lock forward, or when no lock exists.

| Source declared in | Lock | The change arrives when |
|---|---|---|
| Your user file, or `.agents/config.local.yaml` | Your own lock next to it | You run `agent-marketplace update` or `agent-marketplace lock --upgrade` followed by `sync`. |
| The committed `.agents/config.yaml` | The committed `.agents/lock.yaml` | Someone runs `update`, commits the new lock, and each teammate pulls it and runs `sync`. |
| The organization file, with no organization lock | None | Every machine's next plain `sync` fetches the current commit of the ref. No `update` is needed, and nobody reviews the change first. |
| The organization file, with an organization lock | `/etc/agent-marketplace/lock.yaml` | An administrator runs `lock --scope organization --upgrade` and deploys the new lock. Each user's next `sync` then installs it. Consumer `update` never moves this lock. |
| A Marketplace source in a writable file | That file's lock | You run `update`, which moves each pin to the newest usable revision. |
| A Marketplace source in the organization file | The organization lock, or one held in memory | The organization changes the pin. `update` reports these pins as not checked and leaves them alone. |
| A `directory` source | Never locked | Every run reads the folder as it is now. |

Two details are easy to miss. First, editing which resources a group selects usually leaves a Git lock unchanged, because the lock covers every resource at the locked commit. A new selection of a resource that exists at that commit installs straight away. A selection of a resource added upstream after the lock was written is reported as `selected but not found` until the lock moves forward. Second, a plain `lock --scope organization` without `--upgrade` keeps the commit it already records. Only `--upgrade` moves it.

The [reproducibility and lockfiles](./reproducibility-and-lockfiles.md) page explains the lock format and why the CLI never moves a lock on its own.

## What a change does to your selection

The CLI identifies a resource by its kind and name, for example `skill/code-review`. Registries are plain folders, so nothing in them records that one name replaced another. A rename therefore looks to the CLI like two separate changes: the old name disappears and a new name appears. What that means for you depends on how you selected the resource.

| How you selected it | Resource added upstream | Resource removed upstream | Resource renamed upstream |
|---|---|---|---|
| By name, such as `include: [skill/foo]` | Not installed. | `update` refuses, names the selection with its remediation line, and leaves the lock and the old output unchanged. The output is removed with a `selected but not found` warning only when the lock moves without the `update` check, such as after `lock --upgrade`, a deployed organization lock, or a source with no lock. | The same refusal. Once the lock moves without the check, `sync` removes the old output with the same warning, and the new name is not installed. |
| `all: true` or `tags` | Installed, if it matches. | Removed. | The old name is removed and the new name installed. An `exclude` that named the old name no longer matches, so the new name installs even though you excluded the old one. |
| Through a collection | Installed when the collection lists it. | Removed when the collection stops listing it. A member that the collection still lists but that no longer exists makes `update` refuse, the same as a by-name selection; after the lock moves without the check, `sync` removes it with a warning that names the collection. | Follows the collection. If the author renamed the skill but not the collection entry, `update` refuses for the old member name. Your `exclude` of the old name does not carry over to the new name. |
| `required: true` | As for the matching row above. | `update` refuses as above; `sync` fails once the lock moves without the `update` check, or at once when no lock exists. | The same. |

Collections themselves behave like required selections. If the registry renames or deletes a collection that you select with `collection/<name>`, `update` refuses and a `sync` without a lock fails until you change your configuration, and the members stay installed in the meantime.

### Warnings, not errors, for ordinary selections

A selection that no longer exists is a warning, and the run still exits `0`. The consumer usually cannot fix an upstream removal, and failing every sync on every machine would block unrelated work. The warning names the probable causes and the fix:

```text
warning: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
  → this may be caused by an upstream removal, a stale ref pin, a rename, or a cache issue; run 'agent-marketplace status' to diagnose, or 'agent-marketplace remove "foo"' to drop the selection
```

`agent-marketplace update` goes further than the warning. Before it moves any lock it resolves every selection against the new revision, and it refuses to change anything when a selection that resolved at the locked revision no longer resolves. The refusal is part of the `update` command: `sync`, `status`, and a lock the organization deploys do not make this check. `sync --strict` turns the warning into an error and exits `1`. Use it in CI when a stale selection should fail the build.

### Errors for required selections

`required: true` exists to enforce policy, so a required selection that no longer exists fails `sync` instead of warning, and so does a selected collection that no longer exists. `update` refuses before changing anything: it exits `1`, names each missing selection with a remediation line, and leaves the lock, the configuration, and the harness files unchanged:

```text
Error: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
  → required selection: 'agent-marketplace remove' cannot drop it; change the configuration entry that marks it required, or ask that configuration's owner
update left the lock and harness outputs unchanged; rerun 'agent-marketplace update' after resolving the selections above, or update other registries with 'agent-marketplace update --registry <url>'
```

A required miss still fails `sync` when the lock moved without the `update` check. Under an organization lock, that is every machine's next `sync` as soon as the administrator deploys the upgraded lock.

### Hosted Marketplace pins

A Marketplace pin names an exact, immutable revision. If the registry renames or deletes the resource, the pinned revision still installs, and `status` still shows it as `current`. The trouble starts at `update`, which asks the Marketplace for the newest revision of the old name. The Marketplace no longer knows that name, so `update` keeps that pin at its current revision, updates the other pins, and exits `1` with an error such as:

```text
Configured registry entries: 0
Marketplace pins updated: 0
Other Marketplace pins skipped: 1
Marketplace "https://marketplace.example.com" "shared-agents"/"skill"/"foo": skipped ("resource no longer exists on the Marketplace")
Error: 1 Marketplace resource(s) no longer exist:
marketplace https://marketplace.example.com pin shared-agents/skill/foo: resource not found; remove the old pin with `agent-marketplace remove foo` and install the new name
```

Replace the pin to move it; see [how to handle a renamed or removed resource](../how-to/handle-renamed-or-removed-resources.md#_6-replace-a-hosted-marketplace-pin).

Revocation is a separate event: an operator marks a revision as unsafe. A revoked pin fails `sync`. The [respond to a revoked resource](../how-to/respond-to-a-revoked-resource.md) guide covers it.

## What happens to the files

The CLI changes a file in a harness folder only when it can prove that it wrote the file: a symlink into its cache, a marked command file, or a copy listed in the materialization manifest whose content has not changed since. Everything else belongs to you. `sync` leaves it alone, and warns when such a file sits where a resource would go.

This rule shapes what happens to your edits:

- In the default symlink mode, a harness file points into the CLI cache. Editing it edits the cache, and the next `sync` silently restores the registry content. `sync --check` does not detect the edit.
- In file mode, the CLI writes copies. Once you edit a copy, the CLI keeps your bytes and no longer applies upstream updates to that file. Every sync reports the copy with a `materialize.collision.user-owned` warning that calls it a locally edited managed copy and says how to restore the registry version. When you deselect the resource, the CLI keeps the edited copy on disk instead of removing it.

[How to keep a local change to a synced resource](../how-to/keep-local-changes-to-synced-resources.md) shows how to keep an edit without losing upstream updates for the rest.

## Why it works this way

Each choice trades something away:

- **Locks before freshness.** A team gets the same files on every machine and in CI, and an upstream mistake cannot break anyone until someone moves the lock on purpose. The cost is that fixes and new resources also wait for that step. The lockless organization layer makes the opposite trade: administrators push changes to every machine at once, without review or rollback through Git.
- **Names as identity.** Any Git folder can serve as a registry, with no metadata service. The cost is that a rename breaks name-based selections and excludes. Registry authors can soften this; see [how to rename or retire a resource](../how-to/author/rename-or-retire-a-resource.md).
- **Soft failure for ordinary selections, hard failure for required ones.** Ordinary users keep working when a registry drops something, while policy that an organization enforces cannot silently lapse. `update` refuses to move any lock past a selection that disappeared, so one stale entry cannot strand the rest of the update or leave a lock ahead of the files it describes.
- **Pruning only what it can prove.** The CLI never deletes your work, at the price of leaving edited copies behind for you to clean up.

## Related documentation

- [How to handle a renamed or removed resource](../how-to/handle-renamed-or-removed-resources.md)
- [How to review and roll back an update](../how-to/review-and-roll-back-updates.md)
- [How to roll out organization configuration](../how-to/operator/roll-out-organization-configuration.md)
- [Reproducibility and lockfiles](./reproducibility-and-lockfiles.md)
- [Collections reference](../reference/collections.md)
