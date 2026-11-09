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

The GUI SHALL decide whether its version is outdated by the build-kind rules of `upgrade-notice` "Newer releases are detected by build kind", comparing it with the latest stable release on the project's public GitHub Releases, and SHALL display a notice while a newer release is known. It SHALL make no release request and show no notice when `AGENT_MARKETPLACE_NO_UPDATE_CHECK` or `CI` is set to a non-empty value or the effective `update_check.enabled` setting under `upgrade-notice` is `false`. It SHALL NOT download an update until the user activates the notice's update action. A failed version check SHALL be silent. When the build has no package format, or the latest release publishes no asset for this build's package format and architecture, the notice SHALL offer only a link to the release page.

#### Scenario: New stable release is available

- **WHEN** release checking is enabled and the latest stable public GitHub release is newer than the running GUI version
- **THEN** the GUI SHALL display an availability notice naming the newer release
- **AND** it SHALL NOT download the release before the user activates the update action

#### Scenario: Version check fails

- **WHEN** release checking is enabled and the latest stable release cannot be checked
- **THEN** the GUI SHALL continue without an error notice or update download

#### Scenario: Organization disables release checking

- **WHEN** Consumer organization configuration sets `update_check.enabled: false` and user-personal configuration does not set it
- **THEN** the GUI SHALL make no network request for release availability
- **AND** it SHALL show no newer-version notice

#### Scenario: Update checking is suppressed

- **WHEN** `AGENT_MARKETPLACE_NO_UPDATE_CHECK` or `CI` is set to a non-empty value, or user-personal configuration sets `update_check.enabled: false`
- **THEN** the GUI SHALL make no network request for release availability
- **AND** it SHALL show no newer-version notice

#### Scenario: Unpackaged build links to the release page

- **WHEN** a newer release is known and the running GUI was not built as a macOS app, RPM, or DEB package
- **THEN** the notice SHALL offer a link to the release page and no in-app update

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

### Requirement: In-app update on macOS

When a newer release is known, the running GUI is a packaged macOS app that is not running from App Translocation or a mounted disk image, its containing directory is writable, and the release publishes the macOS `.zip` with a `SHA256SUMS` entry, the notice SHALL offer an update action. Activating it SHALL download that release's `.zip` and `SHA256SUMS`, verify the archive digest, and verify that the extracted app has the running app's bundle identifier, the release's version, and a valid code signature. The GUI SHALL then apply the "Quit confirms only unsynced or running work" rules before replacing itself; cancelling SHALL leave the running app unchanged. After an accepted quit it SHALL replace the running app with the verified app and launch the new version. A download, verification, or replacement failure SHALL leave the running app usable and unchanged, SHALL show an error in the notice, and SHALL allow the user to retry.

#### Scenario: User updates the app in place

- **WHEN** an eligible packaged macOS app shows a newer release and the user activates the update with no unsynced or running work
- **THEN** the GUI SHALL download and verify the release's `.zip`
- **AND** it SHALL replace the running app at the same location and launch the new version
- **AND** the new version SHALL report the release version

#### Scenario: Corrupted update download is rejected

- **WHEN** the downloaded `.zip` does not match its `SHA256SUMS` entry, or the extracted app has a different bundle identifier, a different version, or an invalid signature
- **THEN** the GUI SHALL NOT replace the running app
- **AND** it SHALL show an update error in the notice and offer a retry

#### Scenario: Pending work requires confirmation before the update

- **WHEN** the user activates the update while unsynced or running work exists
- **THEN** the GUI SHALL ask for the same confirmation as a quit
- **AND** cancelling SHALL leave the running app and its version unchanged

#### Scenario: Ineligible location falls back to a browser download

- **WHEN** a newer release is known and the packaged macOS app runs from App Translocation, a mounted disk image, or a directory it cannot write
- **THEN** the notice SHALL offer to open the release's `.dmg` download in the browser instead of an in-app update
- **AND** for App Translocation or a mounted disk image, it SHALL tell the user to move the app to Applications to enable updates

### Requirement: Verified package handoff on Linux

When a newer release is known, the running GUI was built as an RPM or DEB package, and the release publishes the package of that format for the running architecture with a `SHA256SUMS` entry, the notice SHALL offer an update action. Activating it SHALL download that package and `SHA256SUMS`, verify the package digest, save the package in the user's download directory, open it with the desktop's default handler, and ask the user to restart the GUI after installing. A package that fails verification SHALL be deleted and SHALL NOT be opened. When the handler cannot be opened, the notice SHALL show the saved package path and a link to the release page.

#### Scenario: User hands the verified package to the software installer

- **WHEN** an RPM- or DEB-packaged GUI shows a newer release and the user activates the update
- **THEN** the GUI SHALL save the verified package of the same format and architecture in the user's download directory
- **AND** it SHALL open the package with the desktop's default handler
- **AND** it SHALL tell the user to restart the GUI after installing

#### Scenario: Unverified Linux package is not opened

- **WHEN** the downloaded package does not match its `SHA256SUMS` entry
- **THEN** the GUI SHALL delete the download and SHALL NOT open it
- **AND** it SHALL show an update error in the notice and offer a retry

### Requirement: Consistent resource opening

For Marketplace, directory, and Git skill rows, Enter and the Open resource button SHALL both open the inspector when it can show the row's content, and neither SHALL be offered when it cannot.

#### Scenario: Enter and Open resource agree across source kinds

- **WHEN** a user selects a Marketplace, directory, or Git skill row and presses Enter or activates Open resource
- **THEN** both MUST open the inspector when it can show that row's content, and neither MUST be offered when it cannot

### Requirement: Search-independent discovery health

Diagnostics SHALL show Marketplace discovery health that does not depend on the resource search query.

#### Scenario: Discovery health ignores the resource search

- **WHEN** a user views Diagnostics with an empty resource search and again with a non-empty resource search
- **THEN** the Marketplace discovery health shown MUST be identical

### Requirement: Resource rows fit the resource list

A resource row SHALL keep its status warning within the row by truncating it, SHALL NOT cause the resource list to scroll horizontally, and SHALL NOT let the warning overlap the source column.

#### Scenario: Long load-failure warning stays inside its row

- **WHEN** a resource row carries a long load-failure warning and the window is 1024 px wide in the light or dark theme
- **THEN** the resource list's scroll width MUST NOT exceed its client width
- **AND** the warning MUST end before the row's source column begins

### Requirement: Row click reveals the inspector

Clicking a resource row SHALL select it and SHALL unfold the inspector when it is folded. Clicking a row while the inspector is open SHALL keep it open. Keyboard selection SHALL NOT change whether the inspector is folded.

#### Scenario: Clicking a row unfolds a folded inspector

- **WHEN** the inspector is folded and the user clicks a resource row
- **THEN** the inspector MUST unfold and show that resource

#### Scenario: Clicking a row keeps an open inspector open

- **WHEN** the inspector is open and the user clicks another resource row
- **THEN** the inspector MUST stay open and show the newly clicked resource

### Requirement: Disabled resource row indicator

A resource row for a disabled resource SHALL render its check indicator in the warning role. Only resources that are on SHALL render the check indicator in the signal (green) role.

#### Scenario: Disabled and enabled rows use distinct indicator roles

- **WHEN** the resource list shows an enabled resource and a disabled resource
- **THEN** the enabled row's check indicator MUST use the signal role
- **AND** the disabled row's check indicator MUST use the warning role and MUST NOT use the signal role

### Requirement: Sidebar source labels

A sidebar Marketplace source SHALL be labeled by its configured source name. When the source has no name, or only the generic name `marketplace` or `marketplace-<n>` that the GUI assigns, it SHALL be labeled by its endpoint host. The full location SHALL stay available in the row's tooltip and accessible name, and the tooltip text SHALL have at least 4.5:1 contrast against its background in the light and dark themes.

#### Scenario: Named Marketplace source shows its name

- **WHEN** the user-personal configuration declares a Marketplace source under a non-generic name
- **THEN** the sidebar row MUST show that name

#### Scenario: Generic or missing name falls back to the host

- **WHEN** a Marketplace source has no name or a generic `marketplace` or `marketplace-<n>` name
- **THEN** the sidebar row MUST show the endpoint host

#### Scenario: Source tooltip is readable in both themes

- **WHEN** the user hovers over or focuses a sidebar source row in the light or dark theme
- **THEN** a tooltip MUST show the source's full location with at least 4.5:1 contrast against its background

### Requirement: Plain-language discovery health

Diagnostics SHALL describe each Marketplace endpoint's discovery state in plain words: whether its results are current, saved from an earlier check, or not checked yet, and whether only part of the catalog is shown. It SHALL NOT show the words "fresh" or "truncated".

#### Scenario: Discovery health uses plain wording

- **WHEN** a user views Diagnostics with Marketplace discovery health for endpoints whose results are current, saved from an earlier check, not checked yet, or partial
- **THEN** each endpoint MUST state in plain words whether its results are current and whether only part of the catalog is shown
- **AND** the Marketplace discovery section MUST NOT contain the words "fresh" or "truncated"
