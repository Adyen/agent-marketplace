# Proposal

## Why

Desktop GUI users with several sources from one repository see a flat list of near-identical entries, and the sidebar source rows and the ＋ button only open Settings → Sources without taking the user to the source they picked or to the add form. Settings → Sources also has no way to narrow a long list.

## What Changes

- Sources in the same layer that share a kind and location with different subpaths render under one indented, foldable parent entry in the sidebar and in Settings → Sources. A source alone at its location is not wrapped. Grouping is presentation only; the configuration format does not change.
- Clicking a sidebar source row opens Settings → Sources, unfolds the group that holds the source, scrolls the source into view, and highlights it.
- The sidebar ＋ next to SOURCES opens Settings → Sources with focus in the Add source form.
- Settings → Sources has a search field that filters sources by label, URL, or path, case-insensitively, shows an empty state when nothing matches, and restores the full list when cleared.

## Capabilities

### New Capabilities

### Modified Capabilities

- `gui-shell`: grouped subpath sources, sidebar source and add-source deep links into Settings → Sources, and source search in Settings → Sources.

## Impact

Desktop GUI frontend only (`gui/src`, `gui/tests`, `gui/DESIGN.md`). No local API, configuration format, CLI, or TUI change.

## Release note

The desktop app groups sources from the same repository under one foldable entry, takes you straight to a source or to the add form from the sidebar, and lets you search the source list in Settings.
