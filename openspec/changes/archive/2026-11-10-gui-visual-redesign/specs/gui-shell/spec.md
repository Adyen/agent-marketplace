## ADDED Requirements

### Requirement: Toolbar update indicator

The toolbar SHALL show the Update action before the Sync action, keeping the accessible names "Update" and "Sync". While the latest update availability check reports more than zero resources, the Update action SHALL show that number as a visible indicator. The indicator SHALL be absent when the number is zero, before any check completes, and when the check fails; a failed check SHALL show no error.

#### Scenario: Update shows the available update count

- **WHEN** an update availability check completes and reports that an update would change resources in the user-personal scope
- **THEN** the toolbar MUST show Update before Sync
- **AND** the Update action MUST show the number of resources an update would change
- **AND** the actions' accessible names MUST remain "Update" and "Sync"

#### Scenario: No update indicator at zero, before a check, or after a failed check

- **WHEN** no update availability check has completed, the latest check reports zero resources, or the latest check fails
- **THEN** the Update action MUST NOT show an available-updates indicator
- **AND** the GUI MUST NOT show an error for the failed check

### Requirement: Read-only update availability check

After each final snapshot the GUI accepts at launch, after an explicit refresh, or after its own completed guarded operation or mutation, the session SHALL check the user-personal scope with the same read-only preview as `agent-marketplace update --dry-run` and report how many resources an update would add or change. The check SHALL NOT write authored configuration, acquisition locks, dependency journals, or materialized harness files, and SHALL NOT delay the snapshot or block guarded actions.

#### Scenario: Update availability check writes nothing

- **WHEN** an update availability check runs while a newer revision exists for a user-personal source
- **THEN** authored configuration, acquisition locks, dependency journals, and materialized harness files MUST be byte-identical before and after the check

### Requirement: Stale update availability results are discarded

The session SHALL discard an update availability result when a guarded operation or mutation started after the check began.

#### Scenario: Update availability result from before a later change is discarded

- **WHEN** a guarded operation or mutation starts after an update availability check began and the check then completes
- **THEN** the session MUST NOT report that check's result

### Requirement: Resizable panes

The sidebar and the inspector SHALL each have a resize handle that is a focusable, named control. Dragging a handle with a pointer, or pressing the arrow keys, Home, or End while it has focus, SHALL change that pane's width, clamped to the pane's minimum and maximum. Resizing SHALL NOT issue a local API request.

#### Scenario: Pointer drag resizes and clamps a pane

- **WHEN** the user drags the sidebar or inspector handle within its range and then beyond its minimum or maximum
- **THEN** the pane width MUST follow the drag within the range
- **AND** the width MUST stop at the pane's minimum or maximum

#### Scenario: Keyboard resizes a pane from its handle

- **WHEN** a pane's resize handle has focus and the user presses the arrow keys, Home, or End
- **THEN** the pane width MUST change in the arrow's direction, or jump to the minimum or maximum
- **AND** the handle MUST expose the current, minimum, and maximum widths to assistive technology

### Requirement: Persisted pane widths

The GUI SHALL persist each pane width in GUI-local storage and restore it on the next launch. It SHALL use the pane's default width when the stored value is missing or malformed or storage is unavailable.

#### Scenario: Pane widths persist across relaunch

- **WHEN** the user resizes a pane and the GUI restarts
- **THEN** the pane MUST open at the width the user chose

#### Scenario: Missing or malformed stored pane width uses the default

- **WHEN** the stored width for a pane is missing, not a number, or storage is unavailable at launch
- **THEN** the pane MUST open at its default width

### Requirement: Plain-language resource action dialog

The resource action dialog SHALL have no header icon. Its primary text SHALL state in plain language what the action will do to the resource and where the resource will be available, without the words "config", "configuration", "lock", "lockfile", "harness", "acquisition", or "materialize". The technical plan preview SHALL sit inside a "Technical details" disclosure that is collapsed when the dialog opens.

#### Scenario: Resource action dialog uses plain language

- **WHEN** the user opens the install dialog for a resource
- **THEN** the dialog MUST have no header icon
- **AND** its primary text MUST name the resource and where it will be available without the words "config", "configuration", "lock", "lockfile", "harness", "acquisition", or "materialize"
- **AND** the plan preview MUST be inside a collapsed "Technical details" disclosure

### Requirement: Resource action progress spinner

The resource action dialog SHALL show a spinner while the plan is being prepared or the confirmed action is in flight, and SHALL hide it when the action succeeds or fails.

#### Scenario: Spinner shows only while the action is in flight

- **WHEN** the user confirms an install and the request is in flight, and the request then succeeds or fails
- **THEN** the dialog MUST show a spinner while the request is in flight
- **AND** the spinner MUST be hidden after the request succeeds or fails
