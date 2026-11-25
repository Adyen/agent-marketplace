# Proposal

## Why

User review of the desktop GUI found duplicated sidebar controls, unexplained count glyphs, a misleading delete icon, an unstyled inspector button, and Marketplace rows whose registry label depends on whether the resource is installed. Each one makes the workspace harder to read without adding capability.

## What Changes

- Merge the sidebar navigation and filter controls: one Resources entry that resets the facets, Needs attention and Required by org status filters with counts, Diagnostics, and one Kind list with counts. Remove the sidebar Search row and the Pending sync row; the toolbar search, the Sync count, and the status bar already carry them.
- List grouping sections in the sidebar only when they are not already covered: omit the section list under Kind grouping and omit the pinned Needs attention section.
- Show pending and available-update counts as plain neutral numbers; drop the `~` prefix and the green update count.
- Rename the resource Remove action to Uninstall in the inspector, its dialog, progress, and notifications, and show Uninstall and Exclude as text buttons rather than a trash icon. The action semantics are unchanged.
- Restyle the inspector action buttons, including Open resource, as outlined buttons with leading icons.
- Label installed and available Marketplace rows with the same `<endpoint>/<registry>` registry label.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `gui-shell`: consolidated sidebar navigation, sidebar grouping list, facet reset entry, plain toolbar and status bar counts, Uninstall naming and inspector action controls.
- `gui-local-api`: Uninstall notification wording and a consistent Marketplace registry label.

## Impact

Affected areas are the Vue sidebar, toolbar, status bar, inspector and resource-action dialog, the local API notification messages and row registry label, GUI unit and Playwright tests, and `gui/DESIGN.md`. The local API schema, the session core, the TUI, and persisted configuration remain unchanged.

## Release note

The desktop sidebar is simpler, counts read as plain numbers, removing a resource is now called Uninstall, and Marketplace resources show the same registry label whether or not they are installed.
