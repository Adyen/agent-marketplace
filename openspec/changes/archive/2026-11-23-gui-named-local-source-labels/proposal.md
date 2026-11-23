## Why

A directory or Git source is shown by its last path segments, so two sources in the same folder or from the same host look alike and the name the user chose is never shown. A configured name is the label the user picked, so it should be what the sidebar shows.

## What Changes

- A directory or Git source with a configured name is labeled by that name in the sidebar, the source browse header, and the source group rows.
- A directory or Git source without a name keeps today's path label.
- Marketplace sources keep their endpoint label whatever their name, as the existing Marketplace endpoint labels requirement states.
- The full location stays in the tooltip and accessible name.

## Capabilities

### Modified Capabilities

- `gui-shell`: sidebar and browse-header labels for named directory and Git sources.

## Impact

Desktop GUI source labeling (`gui/src/source-label.ts`, `gui/src/Sidebar.vue`) and its tests. No configuration, local API, Marketplace, TUI, or resource behavior changes.

## Release note

Directory and Git sources in the desktop app are now labeled by the name you gave them, and fall back to their path when unnamed.
