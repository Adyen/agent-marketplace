## Why

The TUI lets users group resources by Status, Source, or Kind with an optional secondary axis, and pins actionable resources in a Needs attention section. The desktop GUI only offers Status grouping, so users with many sources or kinds cannot organize the list the same way.

## What Changes

- Add a toolbar View menu with Group by (Status, Source, Kind) and Then by (None, Kind, Source excluding the primary axis) choices that apply immediately.
- Render nested sections for the selected grouping, pin Needs attention outside Status grouping, and keep folds separate for each grouping combination.
- Persist the choice in GUI-local storage only; no local API request, TUI preference file, authored configuration, or acquisition lock is written.
- Add a `source` identity and label to each snapshot row so the GUI can group by source with the same identity rules as the TUI.

## Capabilities

### New Capabilities

None.

### Modified Capabilities
- `gui-shell`: Add selectable resource grouping in the workspace list, the toolbar View menu, and local persistence of the grouping choice.
- `gui-local-api`: Snapshot rows carry a source identity and sanitized label.

## Impact

GUI frontend (`gui/src`: grouping, workspace list, sidebar, toolbar, View menu, fixtures, tests, preview, and `gui/DESIGN.md`), the local API snapshot row DTO (`internal/localapi`, which gains `source`), and the generated GUI client from `api/gui-local.yaml`.
