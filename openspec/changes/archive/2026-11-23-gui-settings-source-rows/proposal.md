## Why

Settings → Sources rows show the location as the primary line, so a named source is found by its path, and the uppercase kind tags and always-visible trash buttons add noise to every row. The search field is shown even when there are only a few sources to search.

## What Changes

- A user source with a configured name shows the name as its primary line, with its location as the secondary line.
- Kind tags read "Directory", "Git", "Marketplace", and "Organization" in sentence case.
- The remove control is shown on row hover or keyboard focus only. It stays in the document order and remains reachable by keyboard.
- The "Search sources" field is shown only when more than six sources are configured. Below that, the full list is always shown.
- Organization sources and the removal confirmation keep their existing behaviour.

## Capabilities

### Modified Capabilities

- `gui-shell`: Settings → Sources row layout and the source search threshold.

## Impact

Desktop GUI Settings Sources list (`gui/src/SourceList.vue`, `gui/src/SettingsWindow.vue`, `gui/src/style.css`) and its tests. No configuration, local API, Marketplace, TUI, or resource behaviour changes.

## Release note

Settings → Sources lists each source by its name, shows its location beneath it, and offers search only when there are more than six sources. The remove button appears when you hover over or focus a source.
