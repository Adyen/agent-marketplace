## Why

In short windows the sidebar's Groups section takes space that the Sources list needs, so the Sources list shows fewer than three rows at 800x600. The Groups entries are still needed for navigation, so the section stays but can fold.

## What Changes

- The sidebar Groups section heading is a disclosure control with its expanded state exposed.
- Groups start expanded at window heights of 720 px and above, and collapsed below 720 px.
- A collapsed Groups section keeps its group entries in the document, so the Groups section still exists.

## Capabilities

### Modified Capabilities

- `gui-shell`: the sidebar Groups section can fold, and starts collapsed in short windows.

## Impact

Desktop GUI sidebar (`gui/src/Sidebar.vue`, `gui/src/style.css`) and its tests. No configuration, local API, Marketplace, TUI, or resource behavior changes.

## Release note

The desktop app's sidebar Groups section folds, and starts folded in short windows so Sources keeps room to show its rows.
