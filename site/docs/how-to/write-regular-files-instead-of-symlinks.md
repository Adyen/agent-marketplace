# How to write regular files instead of symlinks

By default, sync installs skills as [symlinks](../glossary.md#symlink) that point into the CLI's cache. Switch to file mode to write regular copies into the repository instead.

## Prerequisites

- A repository with `.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

## 1. Set the materialization mode

Add a `materialization` block to `.agents/config.yaml`:

```yaml
materialization:
  mode: file
```

`mode` accepts `symlink` (the default) or `file`. In `.agents/config.yaml` or `.agents/config.local.yaml`, the setting changes only this repository's output. In `~/.agents/config.yaml`, it sets the default for your machine-wide output and for every repository that does not set its own.

## 2. Override one target

If one harness needs a different mode, set it under `targets`:

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

It prints the usual `Installed <n>  Removed <n>  Malformed <n>  Errors <n>` summary with one `→` line per output root. For the harnesses in file mode, sync writes regular folders and files where it would otherwise create links. It records the paths it wrote in `.agents/materialization-manifest.yaml` (for machine-wide output, `~/.agent-marketplace/materialization-manifest.yaml`), so a later sync removes only files it wrote. If you edit one of those files and the resource is later deselected, sync keeps your edited copy and prints a `materialize.collision.user-owned` warning.

## When to use file mode

- Your harness or another tool does not follow symlinks.
- Your build needs the resource contents inside the working tree.
- You want to inspect or archive the generated files without following links.

## Shared configuration files are not affected

This setting controls resource files and folders. Shared configuration files that collect entries from several sources, such as `.codex/hooks.json`, `.devin/hooks.v1.json`, and `.devin/mcp_config.json`, follow the same ownership rules in either mode.

## Troubleshooting

- If some generated files are still symlinks, a `targets` entry may set `symlink` for that harness. Check `.agents/config.yaml`, `.agents/config.local.yaml`, and `~/.agents/config.yaml`.
