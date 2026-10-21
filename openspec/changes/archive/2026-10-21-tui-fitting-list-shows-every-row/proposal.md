## Why

Rebuilds keep the selected item on its previous screen line so rows inserted above it do not move it (#857). When the list had room for every row, that rule still scrolled the list: with a section header selected, a reload that inserted rows above it hid the top sections above the viewport while the list pane below was blank, so resources looked as if they had disappeared until the user pressed `g`.

## What Changes

- `clampListOffset` shows the list from its first row whenever every row fits within the viewport. Overflowing lists keep the anchored screen line.
- `tui-navigation` adds that a list that fits shows every row. `tui-startup` scopes "verified rows inserted above a selection keep it on the same screen line" to lists that overflow the viewport.
- A TUI QA journey selects a lower section header, adds a selection that inserts rows above it, reloads, and checks that the top sections are visible with the header still selected.

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-navigation`: a list that fits shows every row.
- `tui-startup`: the inserted-rows anchoring scenario applies to overflowing lists.

## Impact

`internal/tui` list viewport clamping (`clampListOffset`), which every list render and rebuild uses. Overflowing lists behave as before.
