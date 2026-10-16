# How to keep a local change to a synced resource

Change a skill or other resource that a registry provides, and keep the change across syncs and updates. Editing the file in your harness folder does not work:

- In the default symlink mode, the harness file points into the CLI cache. The next `sync` silently restores the registry content.
- In file mode, the CLI keeps an edited copy, but stops updating it and warns on every sync.

Instead, keep your version in a folder of your own and tell the CLI to install it in place of the registry's copy. [How upstream changes reach consumers](../explanation/how-upstream-changes-reach-consumers.md#what-happens-to-the-files) explains why edits behave this way.

## Prerequisites

- A repository with `.agents/config.yaml` that selects the resource from a Git source. This guide uses a source named `shared` that provides `skill/foo`.

## 1. Copy the resource into a local folder

A directory source uses the same layout as a registry, so skills go under `skills/<name>/`:

```bash
mkdir -p local-skills/skills
cp -RL .claude/skills/foo local-skills/skills/foo
```

`-L` copies the files that the symlink points to rather than the link. Edit `local-skills/skills/foo/SKILL.md`. Keep `name: foo` in the frontmatter so your copy replaces the registry's.

## 2. Install your copy instead of the registry's

Create `.agents/config.local.yaml`. Repeat the `shared` source to exclude its `foo`, and add your folder as a directory source:

```yaml
apiVersion: agent-marketplace/v4
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: main
  mine:
    directory: ./local-skills
resources:
  shared:
    exclude: [skill/foo]
  mine:
    include: [skill/foo]
```

The source URL and ref must match the committed file. `./local-skills` resolves from the repository root.

## 3. Sync and verify

```bash
agent-marketplace sync
```

Expected output:

```text
Installed 1  Removed 0  Malformed 0  Errors 0
  → /home/alex/project/.claude/skills (1 written)
```

The CLI adds `.agents/config.local.yaml` and `.agents/lock.local.yaml` to `.gitignore`, so your change stays out of the team's configuration. Check which copy is installed:

```bash
agent-marketplace status
```

Under `SYNCED RESOURCES`, the `foo` row names `local-skills` in its `REGISTRY` column. The CLI reads a directory source fresh on every run, so later edits to your copy apply on the next `sync`. `agent-marketplace update` still updates every other resource from `shared` and leaves `foo` alone.

## Share the change with the team

To give everyone the changed version, commit `local-skills/` and put the `mine` source and the `exclude` in `.agents/config.yaml` instead of the local file. If the change would help other users of the registry, propose it upstream; then delete your copy and the extra configuration once the registry publishes it.

## Undo the change

Delete the `shared` exclude and the `mine` source from `.agents/config.local.yaml`, then run `agent-marketplace sync`. The registry's copy returns.

## Return an edited copy to the CLI in file mode

If you edited a copy in file mode and want the registry's version back, delete the copy and sync:

```bash
rm -rf .claude/skills/foo
agent-marketplace sync
```

Expected output:

```text
Installed 1  Removed 0  Malformed 0  Errors 0
  → /home/alex/project/.claude/skills (1 written)
```

The CLI manages the new copy again, and later updates reach it.

## Troubleshooting

- **`warning: skill "foo" is provided by multiple registries; using .../local-skills`**: you added the directory source without the `exclude`. Your copy still wins, because the personal-local file has higher priority, but every sync and check prints the warning. Add the `exclude` from step 2.
- **`warning: materialize.collision.user-owned: local edit: preserved locally edited copy "/home/alex/project/.claude/skills/foo" of resource "foo" (managed by agent-marketplace; its contents changed since the last sync, so registry updates are not applied); to restore the registry version, delete or move it and run agent-marketplace sync`**: you edited a copy that the CLI wrote in file mode. The CLI keeps your version and stops applying upstream updates to that file. Move your version into a directory source as shown above, or delete the copy and sync.
- **`warning: materialize.collision.user-owned: collision: preserved user-owned "foo" (not managed by agent-marketplace); to resolve, exclude this resource (agent-marketplace remove --from <url> "foo") or rename the conflicting file at "<path>"`**: a file that the CLI did not write sits where the resource would go. The CLI keeps it and does not install the registry version. Move your version into a directory source as shown above, or delete the file and sync.
- **Your copy is not installed and `status` shows no `foo` from `local-skills`**: check the layout. The skill must be at `local-skills/skills/foo/SKILL.md`, not `local-skills/foo/SKILL.md`. See the [registry layout reference](../reference/registry-layout.md).

## Related documentation

- [How to use personal repository overrides](./use-personal-repository-overrides.md)
- [How to use a local registry](./use-a-local-registry.md)
- [How to write regular files instead of symlinks](./write-regular-files-instead-of-symlinks.md)
