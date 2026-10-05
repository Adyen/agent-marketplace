## Why

The resource list requires switching between scope, registry, and status views to see related information. One status-grouped view can display scope letters and registry-qualified resource names together while recovering a line for content.

## What Changes

- **BREAKING** Remove the top tab bar and resource-list tab shortcuts and palette actions. The existing status grouping becomes the only view.
- Put a fixed-width letter scope column before the kind and `registry/name` label. Keep the existing status glyph and harness override badge.
- Use registry repository display names without registry subpaths, with `(local)` for resources without a recorded registry.
- Keep search, folding, selection anchoring, provisional rows, toggle guards, and synchronization behavior.
- Drop the obsolete tab attribute from browse action telemetry.
- Update the TUI reference, quick-start, development guidance, and real-terminal journeys.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-navigation`: One status-grouped resource list; former tab shortcuts are inactive; normal list browsing remains available.
- `tui-rendering`: Scope-letter prefix and registry-qualified labels at supported dimensions.
- `tui-startup`: Provisional rows retain their verified layout in the single view.
- `tui-feedback`: Remove tab palette actions and obsolete browse telemetry attributes.

## Impact

Changes stay within TUI navigation and rendering, their tests and real-terminal QA, and corresponding documentation and specs. No dependencies, release version, configuration or acquisition lock format, resource resolution, selection, ownership, or materialization policy changes.
