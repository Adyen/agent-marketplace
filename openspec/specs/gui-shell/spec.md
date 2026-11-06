# gui-shell Specification

## Purpose
Defines the desktop GUI workspace, refresh lifecycle, accessible controls, version notices, telemetry surface, and boundaries on configuration editing.

## Requirements

### Requirement: User-scope-only workspace

The GUI SHALL read and write only the user-personal configuration root and SHALL treat the Consumer organization configuration as read-only. It SHALL NOT read or write repository-committed or personal-local configuration and SHALL NOT depend on the launch directory.

#### Scenario: Launch directory does not change the GUI workspace

- **WHEN** the GUI starts from a directory containing repository-committed or personal-local configuration
- **THEN** it SHALL resolve the same user-personal workspace as when launched elsewhere
- **AND** it SHALL NOT read or write either repository or personal-local configuration
- **AND** Consumer organization configuration SHALL remain unchanged

### Requirement: Single GUI instance

Only one GUI instance SHALL own the desktop workspace. A second launch SHALL focus the running window and hand any launch request to it.

#### Scenario: Second launch focuses the running instance

- **WHEN** the GUI is already running and another launch request is received
- **THEN** the existing window SHALL be focused
- **AND** the launch request SHALL be handed to that instance rather than opening a second workspace

### Requirement: Explicit and focus-triggered refresh

The GUI SHALL provide an explicit refresh action, reload after its own successful mutation, and perform an ordinary reload when the window regains focus. Ordinary reload behavior SHALL follow `session-startup` and retain the current view until final assessment. The GUI SHALL NOT use file watching.

#### Scenario: Refresh and focus reload use ordinary snapshot behavior

- **WHEN** the user refreshes, the window regains focus, or the GUI completes one of its own mutations
- **THEN** the session SHALL reload using the ordinary reload behavior in `session-startup`
- **AND** an accepted resource list SHALL remain visible until the final assessed snapshot is accepted

#### Scenario: External file changes require a refresh trigger

- **WHEN** configuration changes outside the GUI while it is unfocused
- **THEN** the GUI SHALL load the change on an explicit refresh or when the window regains focus
- **AND** it SHALL NOT watch files for changes

### Requirement: Accessible keyboard-operated controls

Every GUI action SHALL be available through visible controls and SHALL be operable using only a keyboard. Controls SHALL have accessible names, and user-facing text SHALL be in English.

#### Scenario: All actions are keyboard reachable and named

- **WHEN** a user operates the GUI without a pointer
- **THEN** every action SHALL be reachable and operable by keyboard
- **AND** every control SHALL expose an accessible name in English

### Requirement: Newer-version availability notice

When enabled by Consumer organization configuration under `config-layering` (default enabled), the GUI SHALL compare its version with the latest stable release on the project's public GitHub Releases and display a notice when a newer version is available. It SHALL NOT download or install an update. A failed version check SHALL be silent.

#### Scenario: New stable release is available

- **WHEN** release checking is enabled and the latest stable public GitHub release is newer than the running GUI version
- **THEN** the GUI SHALL display an availability notice without downloading or installing the release

#### Scenario: Version check fails

- **WHEN** release checking is enabled and the latest stable release cannot be checked
- **THEN** the GUI SHALL continue without an error notice or update download

#### Scenario: Organization disables release checking

- **WHEN** Consumer organization configuration sets `gui.version_check: false`
- **THEN** the GUI SHALL make no network request for release availability
- **AND** it SHALL show no newer-version notice

### Requirement: GUI telemetry surface

GUI telemetry SHALL identify the command surface as `gui` and retain the TUI's action, view, and Resource-kind event semantics. It SHALL obey the same Telemetry level, Community telemetry consent, and Organization telemetry policy as other commands.

#### Scenario: GUI actions use existing telemetry policy

- **WHEN** GUI telemetry is emitted for an action, view, or Resource kind
- **THEN** it SHALL use the corresponding TUI event attributes with command surface `gui`
- **AND** disclosure SHALL follow the effective Telemetry level, Community telemetry consent, and Organization telemetry policy

### Requirement: No raw configuration editor

The GUI SHALL NOT provide an action that edits raw configuration text.

#### Scenario: Configuration actions do not open a raw editor

- **WHEN** a user browses GUI actions or opens configuration settings
- **THEN** no action SHALL open a raw configuration editor

### Requirement: Quit confirms only unsynced or running work

Quitting the GUI from its quit control or a native window close request SHALL exit without a confirmation when there are no pending changes and no operation is running or being requested. When pending changes exist or an operation is running or being requested, the GUI SHALL show the quit confirmation, which keeps its affirmative, negative, and cancel paths.

#### Scenario: Quit with nothing pending exits immediately

- **WHEN** a user quits with zero pending changes and no running or requested operation
- **THEN** the GUI SHALL exit without showing a confirmation
- **AND** no sync SHALL be started

#### Scenario: Quit with pending or running work confirms

- **WHEN** a user quits while pending changes exist or an operation is running or being requested
- **THEN** the GUI SHALL show the quit confirmation before exiting

### Requirement: Selectable GUI view grouping

The GUI SHALL offer a toolbar View menu with Group by choices Status, Source, and Kind and Then by choices None, Kind, and Source excluding the primary axis. A choice SHALL apply immediately; choosing a primary axis equal to the current secondary axis SHALL reset the secondary axis to None. Section identity, labels, ordering, nesting, the pinned Needs attention section, leaf ordering, and search behavior SHALL follow the grouping requirements of `tui-navigation`. Folds SHALL be session-only and kept separately for each grouping combination. Changing grouping SHALL keep the selected resource when it is visible, otherwise select its deepest visible enclosing section, otherwise the first item. The sidebar SHALL list the top-level sections of the current grouping. The GUI SHALL persist the choice in GUI-local storage, restore it on the next launch, and use Status and None when the stored value is absent, malformed, or invalid, or when storage is unavailable. Choosing a grouping SHALL NOT issue a local API request or write the TUI view preference file, authored configuration, or acquisition locks.

#### Scenario: Every valid grouping combination retains distinct resources

- **WHEN** the user selects each valid Group by and Then by combination over resources with mixed kinds, sources whose labels are equal but whose refs or subpaths differ, unknown kinds, and attention statuses
- **THEN** every resource SHALL appear once, sections SHALL nest and order by the selected axes with Needs attention pinned first outside Status grouping, empty sections SHALL be absent, the sidebar SHALL list the top-level sections, and the primary axis SHALL NOT be offered as Then by

#### Scenario: Grouping changes preserve folds and selection

- **WHEN** a resource is selected and the user switches to a grouping whose enclosing section is folded, returns to an earlier grouping, and applies and clears a search
- **THEN** the selection SHALL follow the resource or its deepest visible enclosing section, and each grouping combination SHALL restore its own folds

#### Scenario: Grouping choice persists locally without workspace writes

- **WHEN** the user changes the grouping and the GUI restarts, or the stored choice is malformed, invalid, or unavailable
- **THEN** the GUI SHALL restore the stored choice or use Status and None, and changing the grouping SHALL issue no local API request
