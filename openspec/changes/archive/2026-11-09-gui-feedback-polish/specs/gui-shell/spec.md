## ADDED Requirements

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
