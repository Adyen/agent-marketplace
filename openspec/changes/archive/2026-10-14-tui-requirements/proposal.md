## Why

The resolver selects or previews prerequisites and the CLI reports them, but the TUI shows no prerequisite relationships and gives no feedback when a toggle changes what is required. TUI users cannot tell why a resource is selected, what it needs, or what a toggle brought along or left unmet.

## What Changes

- Add a "Requires" detail section with one met, implied, preview, excluded, or unmet marker per declared prerequisite.
- Add a "Required by" detail section from prerequisite provenance, separate from collection provenance.
- Add a `req` list badge for implied resources and a warning marker for rows with an unmet prerequisite.
- When expansion is enabled, toggling off an implied or still-required resource writes the effective exclusion through the existing serialized configuration mutation and reports each resulting unmet prerequisite; advisory mode writes no prerequisite-specific exclusion.
- Toggling a resource on reports the prerequisites that came along with it, or would with expansion enabled.
- Add a deterministic TUI QA journey and reconcile published TUI prerequisite guidance.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-rendering`: render prerequisite detail sections, the `req` badge, and the unmet warning marker.
- `tui-interactions`: apply prerequisite-aware toggle-off exclusions and toggle feedback.

## Impact

internal/tui detail, badges, rows, footer, and toggle paths; tools/tui-qa journey; TUI and prerequisite documentation. No resolver, pipeline, CLI command, configuration-schema, acquisition-lock, Marketplace, or web change. Collection Via parsing is unchanged.
