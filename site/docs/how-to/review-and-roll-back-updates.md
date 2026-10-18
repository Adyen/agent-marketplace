# How to review and roll back an update

Look at what an update will change before you apply it, and return to the previous versions if it breaks something. This guide uses a repository with a committed `.agents/lock.yaml`. `update` has no dry-run flag, so the review uses `lock --upgrade` and `sync --check` instead.

## Prerequisites

- A repository with `.agents/config.yaml` and `.agents/lock.yaml` committed, and no uncommitted changes to either file.
- At least one remote Git source. Directory sources are never locked, so you cannot review or roll them back this way.

## 1. Move the lock without touching files

Work on a branch, then move the locks to the current commits of their refs:

```bash
git switch -c update-agent-resources
agent-marketplace lock --upgrade
```

Expected output:

```text
Locks: 1 changed
```

`Locks: 0 changed` means nothing upstream moved, and you can stop here. The default scope also covers your user and personal-local locks, so the count can be higher than one. To move one registry only, add `--registry <url>`.

## 2. Review the change

See which commits moved:

```bash
git diff -- .agents/lock.yaml
```

Each changed `commit:` line marks a registry that moved. Under it, a resource with a new `content_digest` changed content, a new `name` entry is a resource added upstream, and a removed `name` entry is a resource that disappeared or was renamed. Read the registry's changelog for the commit range.

Then see which harness files a sync would write or remove:

```bash
agent-marketplace sync --check
```

Typical output:

```text
sync: 2 out of sync (2 writes)
  → /home/alex/project/.claude/skills
      + foo
      + keep
Error: 2 outputs out of sync
```

`+` marks a file sync will write and `-` a file it will remove. The list includes every selected resource from a moved registry, even one whose content did not change, because each commit gets its own cache folder. Use the `content_digest` lines from the diff to tell real content changes apart. A `-` line for a resource you still select usually means it was renamed or removed upstream; see [how to handle a renamed or removed resource](./handle-renamed-or-removed-resources.md).

## 3. Apply and commit

```bash
agent-marketplace sync
agent-marketplace status
```

Expected `sync` output:

```text
Installed 2  Removed 0  Malformed 0  Errors 0
  → /home/alex/project/.claude/skills (2 written)
```

Check the `WARNINGS` section of `status` for `selected but not found`. Try the updated resources in your harness, then commit the lock and open a pull request so reviewers see the same diff:

```bash
git add .agents/lock.yaml
git commit -m "Update agent resources"
```

`agent-marketplace update` does steps 1 and 3 in one command. Use it when you do not need to review first.

## 4. Roll back

To undo an update that you have not committed, restore the lock and sync:

```bash
git checkout HEAD -- .agents/lock.yaml
agent-marketplace sync
```

Expected output:

```text
Installed 2  Removed 0  Malformed 0  Errors 0
  → /home/alex/project/.claude/skills (2 written)
```

To undo an update that is already committed, revert the commit and sync:

```bash
git revert <commit>
agent-marketplace sync
```

Confirm that the files now match the restored lock:

```bash
agent-marketplace sync --locked
```

Expected output:

```text
Installed 0  Removed 0  Malformed 0  Errors 0
```

Teammates return to the old versions with a plain `sync` after they pull the revert.

## Roll back your user-wide resources

`~/.agents/lock.yaml` is not in Git. Copy it before you update, and copy it back to roll back:

```bash
cp ~/.agents/lock.yaml ~/.agents/lock.yaml.before-update
agent-marketplace update --scope user
# to roll back:
cp ~/.agents/lock.yaml.before-update ~/.agents/lock.yaml
agent-marketplace sync
```

Organization resources follow the organization lock, which only its administrator can change.

## Troubleshooting

- **`sync --check` or `sync --locked` reports `lock.snapshot-unavailable` after a rollback**: the old commit is no longer in your cache, for example after `agent-marketplace gc`. A plain `sync` fetches it again. If the registry rewrote its history and the commit no longer exists, you cannot return to it; pin a tag or another commit instead.
- **`sync --check` lists nothing but `Locks: 1 changed`**: the moved registry provides none of your selected resources. Commit the lock anyway so every machine records the same commit.

## Related documentation

- [How to keep resources reproducible](./keep-resources-reproducible.md)
- [Reproducibility and lockfiles](../explanation/reproducibility-and-lockfiles.md)
- [How upstream changes reach consumers](../explanation/how-upstream-changes-reach-consumers.md)
