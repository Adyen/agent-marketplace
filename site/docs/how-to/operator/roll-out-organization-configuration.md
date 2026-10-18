# How to roll out organization configuration

Change the organization configuration on managed machines, or pick up changes that a registry made to resources and collections the organization selects, and check what users will get before they get it. The organization files are `/etc/agent-marketplace/config.yaml` and the optional `/etc/agent-marketplace/lock.yaml`. Every user on a machine receives the resources they select, in their home folder.

## Prerequisites

- A configuration management tool that deploys files to `/etc/agent-marketplace/` on managed machines.
- A staging machine with the same setup, where you have root access.
- The file rules from the [configuration layers reference](../../reference/configuration-layers.md#organization-trust-rules): both files and the folder are owned by root, are not symlinks, and are not writable by group or others.

## 1. Choose whether to deploy a lock

The organization lock decides when registry changes reach users:

- **With a lock**, users install exactly the commits it records. Registry pushes reach nobody until you upgrade and redeploy the lock. You can review and roll back each change.
- **Without a lock**, every user's next `sync` fetches the current commit of each ref. A registry push, including a renamed or deleted resource, reaches every machine without review. If you choose this, point `ref` at a release tag rather than a branch, and roll out by changing the tag.

The rest of this guide assumes a lock. The steps without a lock are the same except step 3.

## 2. Edit the configuration on the staging machine

Edit `/etc/agent-marketplace/config.yaml`, for example to select another collection:

```yaml
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: main
resources:
  shared:
    include:
      - collection/team
      - skill/review
```

A new selection of a resource that exists at the locked commit needs no new lock. A changed source, `ref`, or `path` makes the existing lock stale. Every command except `lock --scope organization` then refuses with `lock.stale` until you write the new lock:

```text
Error: lock.stale: organization lock "/etc/agent-marketplace/lock.yaml": lock does not match configuration
```

You never delete the lock file. The next step shows how `lock --scope organization` reconciles it.

## 3. Write or reconcile the lock

To write a lock where none exists, or to reconcile a stale lock by resolving only the entries whose source or `ref` changed while every other entry keeps its locked commit:

```bash
sudo agent-marketplace lock --scope organization
```

To move every organization source to the current commit of its ref, which picks up new, changed, renamed, and deleted resources and collection edits:

```bash
sudo agent-marketplace lock --scope organization --upgrade
```

Both print `Locks: 1 changed`, or `Locks: 0 changed` when nothing moved. Without `--upgrade`, only entries whose source or `ref` is not recorded in the lock are resolved; every still-configured entry keeps its locked commit, so a resource that the registry added after the lock was written stays unavailable. Neither command checks your selections against the new commit, so a renamed or deleted resource produces no warning here. A malformed or untrusted lock is still refused rather than replaced.

## 4. Check what users will get

As an ordinary user on the staging machine, run:

```bash
agent-marketplace status
```

Read these sections:

- `MANAGED INSTALLATION` shows `validation: valid`. Anything else means users' commands will refuse to run.
- `SYNCED RESOURCES` lists what users will have. `SOURCE` reads `ORG` for organization resources, and `VIA` names the collection that provides them.
- `ORPHANED FILES` lists resources that users will lose on their next sync.
- `WARNINGS` lists selections that the new commit no longer provides:

  ```text
  warning: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
  ```

- `DRIFT` counts files that the next sync will write and remove, for example `3 file(s) to write, 1 file(s) to remove`.

Fix every warning before you deploy. An ordinary selection of a missing name only removes the resource for users. A missing name in a group with `required: true` fails every user's `sync`:

```text
Installed 0  Removed 0  Malformed 0  Errors 1
Error: error: skill "foo" was selected but not found in https://github.com/example-org/agent-resources.git@main
```

In that state no user receives any change, including the rest of the update, until you fix the configuration. To check for it, run `agent-marketplace sync --strict` on the staging machine: it exits `1` on any missing selection.

To follow a rename, change the old name to the new one in the configuration. If the new name exists at the locked commit, you do not need to lock again. Then run `status` once more.

## 5. Deploy the configuration and lock together

Copy both files from the staging machine into your configuration management, and deploy them to managed machines in one change. A configuration with a changed source or `ref` next to the old lock makes every user's `sync` and `status` fail with `lock.stale` until the matching lock arrives. Users receive the change on their next `agent-marketplace sync`:

```text
Installed 3  Removed 1  Malformed 0  Errors 0
  → /home/alex/.claude/skills (2 written, 1 removed)
```

Consumer `update` never changes the organization lock, so users cannot pull organization changes ahead of you.

## 6. Verify on a managed machine

```bash
agent-marketplace status
```

Expected first lines:

```text
MANAGED INSTALLATION
true
path: /etc/agent-marketplace/config.yaml
validation: valid
```

`EXACT REGISTRY LOCKS` shows the commit you deployed, with `ORG` in the `SOURCE` column.

## Roll back

Redeploy the previous configuration and lock from your configuration management. Users return to the previous resources on their next `sync`.

## What users can still change

- Users can exclude an optional organization resource in `~/.agents/config.yaml`. After a rename, their exclude of the old name stops matching, so the new name installs for them.
- Users cannot exclude or remove a resource from a group with `required: true`.
- `update` reports organization-managed Marketplace pins as not checked and leaves them unchanged.

## Related documentation

- [How upstream changes reach consumers](../../explanation/how-upstream-changes-reach-consumers.md)
- [Configuration layers reference](../../reference/configuration-layers.md)
- [How to rename or retire a resource](../author/rename-or-retire-a-resource.md)
