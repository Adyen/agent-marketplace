## Why

When several configuration layers configure the same source location, `config.MergeLayers` keeps one entry per layer. The resolver reports a not-found selection once per location and keeps only the first entry's error. If an optional entry is ordered before a later `required: true` entry, the retained error is non-required, so a required miss is treated as a soft warning by `sync`, `status`, and the TUI.

## What Changes

- The resolver upgrades the retained not-found or malformed selection error to required when any later entry for the same location requires the resource.
- The TUI marks a row soft only when its merged entry is not required.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-selection`: A required miss stays a hard error regardless of which layer lists the optional entry first.

## Impact

Resolver selection diagnostics (`internal/resolver/resolver.go`) and TUI unavailable-row classification (`internal/tui/actions.go`), with resolver and TUI regression tests. No configuration schema, merge order, or CLI output format changes.
