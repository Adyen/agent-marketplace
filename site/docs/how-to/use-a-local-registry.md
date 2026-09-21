# How to use a local registry

Use a directory on the local machine as a registry.

## Prerequisites

- A local directory containing resources in a supported harness layout.
- `agent-marketplace` on your `PATH`.

## 1. Add the local directory to a configuration layer

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

In repository `.agents/config.yaml` and `.agents/config.local.yaml` roots, relative paths resolve from the repository root. Use `directory: .` for the repository itself, `directory: ./skills` for its `skills/` directory, or `directory: ./resources/skills` for a nested registry. These paths stay portable when another developer clones the repository.

In user/organization configs and included fragments, relative paths resolve from the directory containing the declaring file. Relative paths and symlinks cannot escape the applicable base directory; absolute directory paths retain their existing meaning.

## 2. Sync

```bash
agent-marketplace sync
```

Local registries are read live and are not represented in `.agents/lock.yaml`.

## 3. Verify the materialization

```bash
agent-marketplace status
```

Under **LOCAL REGISTRIES**, the local entry appears with its resolved path.

## Boundaries

- User-wide configuration cannot contain repository-relative paths.
- Directory sources are live, unlocked, and unversioned.
- Directory sources are not checked against `allowed_registries`.

## Troubleshooting

- **`path` cannot escape repository root**: Use an absolute path, a home-relative path, or a path that stays within the repository.
- **Resources are not discovered**: Ensure the directory contains a recognized harness layout or set `source_harness` explicitly.
