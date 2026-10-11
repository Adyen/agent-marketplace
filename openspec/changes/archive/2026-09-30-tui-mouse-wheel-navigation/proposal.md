## Why

PR #342 enables mouse-wheel input in the terminal UI. The behavior needs a normative OpenSpec contract, not just the legacy TUI product document, so the spec gate and scenario traceability can verify it.

## What Changes

- Enable terminal mouse reporting while the TUI is active and restore terminal modes on exit.
- Move the resource-list or bundle-tree selection by one entry per vertical wheel event, and scroll the file preview, help, errors, and Marketplace consent by one line per event.
- Ignore clicks, releases, drags, horizontal-wheel events, and wheel input below the supported terminal size; keep Shift+vertical-wheel vertical and preserve the detail scroll anchor across refreshes.
- Document the terminal text-selection trade-off and link the new behavior scenarios to TUI tests.

## Capabilities

### New Capabilities

- `tui-mouse-navigation`: terminal mouse-wheel routing and restoration.

### Modified Capabilities

None.

## Impact

Only the terminal UI and its tests are affected. Mouse input does not modify resource selection in configuration, materialized resources, or terminal settings after the TUI exits.
