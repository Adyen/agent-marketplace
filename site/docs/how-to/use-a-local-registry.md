# How to use a local registry

This guide shows how to use a directory on the local machine as a registry.

## Prerequisites

- A local directory containing resources in a supported harness layout.
- `agent-marketplace` on your `PATH`.

## 1. Add the local directory to a configuration layer

Use `path` instead of `url` in `.agents/config.yaml`:

```yaml
registries:
  - path: ./agent-resources
    skills:
      include:
        - local-review
```

Relative paths resolve from the directory that contains `.agents/config.yaml` and cannot escape it.

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
- Plain local-directory registries are unlocked and unversioned.
- Local paths are not checked against `allowed_registries`.

## Troubleshooting

- **`path` cannot escape repository root**: Use an absolute path, a home-relative path, or a path that stays within the repository.
- **Resources are not discovered**: Ensure the directory contains a recognized harness layout or set `source_harness` explicitly.
