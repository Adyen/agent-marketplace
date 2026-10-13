# How to use a local registry

Use a folder on your computer as a [registry](../glossary.md#registry), for example while you write your own skills.

## Prerequisites

- A local folder containing resources in a supported harness layout.
- `agent-marketplace` on your `PATH`.

## 1. Add the local folder to a configuration file

Declare a `directory` source in `.agents/config.yaml`:

```yaml
apiVersion: agent-marketplace/v4
sources:
  local:
    directory: ./agent-resources
resources:
  local:
    include:
      - skill/local-review
```

In the repository files `.agents/config.yaml` and `.agents/config.local.yaml`, a relative path starts at the repository root, not at the `.agents` folder. Use `directory: .` for the repository itself, `directory: ./skills` for its `skills/` folder, or `directory: ./resources/skills` for a deeper folder. These paths keep working when a teammate clones the repository somewhere else.

In `~/.agents/config.yaml`, in the organization configuration, and in included fragments, a relative path starts at the folder that contains the file. A relative path, and any symlink along it, must stay inside that starting folder. Absolute paths such as `/home/me/agent-resources` are used exactly as written. The CLI does not expand `~`, so write the full path to a folder in your home directory. A `directory` source accepts `source_harness`, but not `ref` or `path`.

## 2. Sync

```bash
agent-marketplace sync
```

Sync reads the folder as it is at that moment and prints the usual `Installed <n>  Removed <n>  Malformed <n>  Errors <n>` summary with one `→` line per output root. Directory sources never appear in `.agents/lock.yaml`.

## 3. Verify the result

```bash
agent-marketplace status
```

Under `SYNCED RESOURCES`, the resource's `REGISTRY` column shows the resolved absolute path of the folder, and its `CACHE` column shows `local`. Typical output (`/abs/repo` stands for your real paths; column widths vary):

```text
SYNCED RESOURCES
KIND   NAME          HARNESS  SOURCE  VIA  TARGET                                      REGISTRY                          REF  CACHE
skill  local-review  claude   REPO    -    /abs/repo/agent-resources/skills/...        /abs/repo/agent-resources              local
```

## Boundaries

- A relative path in `~/.agents/config.yaml` starts at `~/.agents`, so it cannot point into a repository by a repository-relative path.
- Directory sources are read live and have no lock or version. Every sync picks up the folder's current contents.
- `allowed_registries` does not apply to directory sources.

## Troubleshooting

- If sync reports `v4.directory-path` with `relative directory source must remain within its source base directory`, the relative path leaves the repository (or the folder of the declaring file). Use an absolute path, or move the resources inside that folder.
- If no resources are discovered, check that the folder uses a recognized harness layout, or set `source_harness` on the source.
