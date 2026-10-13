## Why

The single status list makes large mixed-resource catalogs difficult to browse by kind or configured source. Users also need to disable the eligible resources under a source section without opening a dialog for each resource.

## What Changes

- Keep Status as the default and offer primary Status, Source, or Kind grouping with optional None, Kind, or Source secondary grouping, excluding the primary axis.
- Pin actionable statuses in Needs attention when not grouped primarily by Status; preserve search, folding, and resource selection.
- Add a View settings modal with live preview and per-user versioned XDG state persistence.
- Change Space on a section header from folding to an explicit, confirmed bulk toggle limited to matching eligible resources, including folded descendants.
- Apply bulk per-resource selections atomically to one layer, retain existing guarded operations and authoritative reload, and expose help and privacy-safe action telemetry.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-navigation`: selectable groupings, search metadata, grouping-specific folds and selection continuity without tabs.
- `tui-interactions`: previewable view settings and confirmed atomic section toggles.
- `tui-rendering`: bounded source/kind sections and new modals.
- `tui-feedback`: help and palette discoverability, bulk counts and telemetry.
- `tui-startup`: preference loading and safe fallback.

## Impact

Changes are confined to TUI presentation, its existing layer-editing transaction integration, direct terminal journeys, and relevant documentation. Config and acquisition lock formats, resolution, ownership, materialization, and selection policy remain unchanged. Source-wide future-resource selection, tabs, filter rails, and resource directory grouping are out of scope.
