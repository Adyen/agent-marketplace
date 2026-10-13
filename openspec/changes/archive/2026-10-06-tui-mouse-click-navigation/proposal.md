## Why

The TUI already receives mouse events for wheel navigation and right-hand text selection, but a plain click does nothing. Users who scroll with the mouse expect to pick a resource or a bundle file by clicking it instead of switching to the keyboard.

## What Changes

- An unmodified left click on a resource-list row selects it; a click on the already-selected resource opens its details, and a click on a section header folds or unfolds it.
- An unmodified left click on a bundle-tree entry selects it and loads its preview; a click on a directory also expands or collapses it.
- A click acts only when the press and release land in the same cell, so drags starting in the list or tree still do nothing.
- Clicks elsewhere (header, search row, footer, empty rows, the right-hand pane, overlays, dialogs, the command palette, while typing a search, or below the minimum size) remain ignored.
- Wheel routing, frame pacing, and right-hand text selection are unchanged.

## Capabilities

### New Capabilities

None

### Modified Capabilities

- `tui-mouse-navigation`: add click navigation for the resource list and bundle tree, and narrow the ignored-click scenario accordingly.

## Impact

TUI input handling, its Go tests, a real-terminal QA journey, and the published TUI mouse guidance. No configuration, lockfile, filesystem, dependency, or mouse-reporting-mode changes.
