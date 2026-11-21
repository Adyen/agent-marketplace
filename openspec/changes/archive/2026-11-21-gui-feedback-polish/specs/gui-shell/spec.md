## ADDED Requirements

### Requirement: Consolidated sidebar navigation

The sidebar SHALL offer each navigation control once: a Resources entry, Needs attention and Required by org status filters, a Kind filter list, and Diagnostics with its count. Each filter entry SHALL show how many rows within the active source it matches, ignoring search and the other facets. The sidebar SHALL NOT offer a search or pending-sync entry; the toolbar, Command-K, the Sync action, and the status bar carry them.

#### Scenario: Sidebar offers each navigation control once

- **WHEN** the workspace shows rows of several kinds, including attention and organization-required rows, under each grouping
- **THEN** the sidebar MUST show one Resources entry, the Needs attention and Required by org entries, one entry per kind, and the Diagnostics entry, each filter entry with its row count
- **AND** it MUST NOT show a search entry, a pending-sync entry, or a second kind or status control

### Requirement: Plain sync and update counts

The Sync action and the status bar SHALL show the pending-sync count as a plain number without a `~` or other prefix, and the status bar SHALL read "<N> pending sync". The Update action's available-update count SHALL use the same neutral tone as the Sync count and SHALL NOT use the signal color.

#### Scenario: Counts render as plain neutral numbers

- **WHEN** one resource is pending sync and an update availability check reports two resources
- **THEN** the Sync action MUST show "1" and the status bar "1 pending sync", neither with a `~`
- **AND** the Update action MUST show "2" in the neutral count tone rather than the signal color

### Requirement: Uninstall resource action

The GUI SHALL name the action that deselects an installed resource Uninstall: inspector control and dialog title "Uninstall Resource", confirm control "Confirm uninstall", dialog text beginning "Uninstall <displayName>", and progress "Uninstalling <displayName>…". Every inspector resource action, including Exclude and Open resource, SHALL be a visibly labeled text button, never only a trash or delete icon.

#### Scenario: Installed resource offers a labeled Uninstall

- **WHEN** the user selects an installed resource that the user can deselect and activates its inspector action
- **THEN** the inspector MUST show a text button labeled Uninstall with the accessible name "Uninstall Resource" and no trash icon
- **AND** the dialog MUST be titled "Uninstall Resource", its text MUST begin "Uninstall <displayName>", and its confirm control MUST be "Confirm uninstall"

## MODIFIED Requirements

### Requirement: Selectable GUI view grouping

The GUI SHALL offer a toolbar View menu with Group by choices Status, Source, and Kind and Then by choices None, Kind, and Source excluding the primary axis. A choice SHALL apply immediately; choosing a primary axis equal to the current secondary axis SHALL reset the secondary axis to None. Section identity, labels, ordering, nesting, the pinned Needs attention section, leaf ordering, and search behavior SHALL follow the grouping requirements of `tui-navigation`. Folds SHALL be session-only and kept separately for each grouping combination. Changing grouping SHALL keep the selected resource when it is visible, otherwise select its deepest visible enclosing section, otherwise the first item. The sidebar SHALL list the top-level sections of the current grouping other than the pinned Needs attention section, which its Needs attention filter covers; under Kind grouping it SHALL omit the section list, which its Kind filter list covers. The GUI SHALL persist the choice in GUI-local storage, restore it on the next launch, and use Status and None when the stored value is absent, malformed, or invalid, or when storage is unavailable. Choosing a grouping SHALL NOT issue a local API request or write the TUI view preference file, authored configuration, or acquisition locks.

#### Scenario: Every valid grouping combination retains distinct resources

- **WHEN** the user selects each valid Group by and Then by combination over resources with mixed kinds, sources whose labels are equal but whose refs or subpaths differ, unknown kinds, and attention statuses
- **THEN** every resource SHALL appear once, sections SHALL nest and order by the selected axes with Needs attention pinned first outside Status grouping, empty sections SHALL be absent, the sidebar SHALL list the top-level sections other than Needs attention and none under Kind grouping, and the primary axis SHALL NOT be offered as Then by

#### Scenario: Grouping changes preserve folds and selection

- **WHEN** a resource is selected and the user switches to a grouping whose enclosing section is folded, returns to an earlier grouping, and applies and clears a search
- **THEN** the selection SHALL follow the resource or its deepest visible enclosing section, and each grouping combination SHALL restore its own folds

#### Scenario: Grouping choice persists locally without workspace writes

- **WHEN** the user changes the grouping and the GUI restarts, or the stored choice is malformed, invalid, or unavailable
- **THEN** the GUI SHALL restore the stored choice or use Status and None, and changing the grouping SHALL issue no local API request

### Requirement: Stackable workspace facets

Resources SHALL support one kind and one status facet at a time, intersected with source and search. Kind and status SHALL be independently clearable; the sidebar Resources entry SHALL show Resources and clear kind, status and source but retain search. Facet changes SHALL NOT change persisted configuration, grouping preferences or session-wide counters. Needs attention SHALL select stale, broken and malformed rows; Required by org SHALL select rows marked required.

#### Scenario: Workspace facets intersect and clear independently

- **WHEN** a user combines a source, kind or status and a search
- **THEN** only their intersection MUST be shown, clearing one MUST preserve the others, and the Resources entry MUST reset the facets without clearing search.

### Requirement: Conditional resource confirmation
The GUI SHALL prepare every inspector resource action through its existing guarded plan flow. Install and Restore SHALL confirm a current plan immediately without a dialog when risk acceptance is not required. Uninstall and Exclude SHALL always open the dialog. A plan preparation failure SHALL open the dialog with its error and Retry plan. The installed resource SHALL retain the existing status-group selection behavior.

#### Scenario: Plain install and restore skip the dialog
- **WHEN** the user activates Install or Restore and its current plan needs no risk acceptance
- **THEN** the GUI SHALL confirm the plan without opening a dialog and write the selection through the existing action endpoint
- **AND** the action button SHALL show a busy state with a progress cursor during planning and submission

#### Scenario: Risk and preparation failure keep the dialog
- **WHEN** an Install plan requires Marketplace risk acceptance or preparing an Install or Restore plan fails
- **THEN** the GUI SHALL open the dialog with the unchecked risk acknowledgement or the error and Retry plan respectively
- **AND** no action SHALL be submitted before risk acceptance or a successful retry

#### Scenario: Remove and exclude keep confirmation
- **WHEN** the user activates Uninstall or Exclude
- **THEN** the GUI SHALL open its confirmation dialog and SHALL NOT submit the action until confirmation
