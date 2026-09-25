# How to write regular files instead of symlinks

Switch from the default symlink mode to regular files for repository output.

## Prerequisites

- A repository with `.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

## 1. Set the materialization mode

Add a `materialization` block to `.agents/config.yaml`:

```yaml
materialization:
  mode: file
```

## 2. Override one target

If one harness needs a different mode, use `targets`:

```yaml
materialization:
  mode: file
  targets:
    devin: symlink
```

## 3. Sync

```bash
agent-marketplace sync
```

The CLI writes regular directories and files instead of links for the configured targets. It tracks managed paths in `.agents/materialization-manifest.yaml` so stale files can be removed safely.

## When to use file mode

- The harness or toolchain does not follow symlinks.
- The build process needs the resource contents in the working tree.
- You want to inspect or archive the generated files without resolving links.

## Shared configuration is not affected

This setting controls generated resource files and trees. Shared aggregate files such as `.codex/hooks.json`, `.devin/hooks.v1.json`, and `.devin/mcp_config.json` retain their ownership reconciliation rules in either mode.

## Troubleshooting

- **Generated files still contain symlinks**: A `targets` override may still be using `symlink` for that harness. Check `.agents/config.yaml`.
