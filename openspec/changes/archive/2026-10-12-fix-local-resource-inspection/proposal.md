## Why

Local disk resources can be discovered successfully but fail to open in the TUI because inspection uses a bundle-relative source path against the consumer working directory. This breaks both the bundle tree and content preview for absolute and relative local registry configurations.

## What Changes

- Anchor local resource inspection to the source bundle that was ingested.
- Keep primary and supporting-file previews independent of the consumer working directory and reject substitution by same-named consumer files.
- Preserve visible failures for missing or unsafe content, read-only inspection, and existing Marketplace and exact Git snapshot source identity.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-navigation`: Specify source-anchored local resource browsing and its failure behavior.

## Impact

Disk-backed catalog loading and TUI source inspection tests. No configuration schema, installation, selection, preview-limit, or symlink-policy changes.
