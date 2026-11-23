## ADDED Requirements

### Requirement: Settings source rows lead with the name

A Settings → Sources user source row SHALL show its configured name as the primary line when the name is non-empty, and its location as the secondary line. A source without a configured name SHALL show its location as the primary line. Kind tags SHALL read "Directory", "Git", "Marketplace", and "Organization" in sentence case.

#### Scenario: Settings source rows lead with the name

- **WHEN** a user source with a non-empty name is listed in Settings → Sources
- **THEN** the row MUST show the name as its primary line and the location as its secondary line
- **AND** the kind tag MUST read in sentence case, for example "Directory"

### Requirement: Source row remove control on hover and focus

A Settings → Sources user source row SHALL show its remove control only while the row is hovered or has keyboard focus within it. The control SHALL remain in the document and reachable by keyboard at all times.

#### Scenario: Source row remove stays reachable on hover and focus

- **WHEN** a user source row is not hovered and has no focus within it
- **THEN** its remove control MUST be visually hidden
- **AND** the control MUST remain focusable by keyboard

## MODIFIED Requirements

### Requirement: Source search in Settings

Settings → Sources SHALL offer a search field when more than six sources are configured across the user and organization layers. Below that count, the search field SHALL NOT be shown and every source SHALL be listed. The search field SHALL filter user and organization sources to those whose label, location, or subpath contains the query, ignoring case. A group SHALL stay while any of its sources matches and SHALL show only matching sources. When no source matches, Settings → Sources SHALL show an empty state naming the query. Clearing the field SHALL restore the full list. Searching SHALL NOT issue a local API request.

#### Scenario: Search filters sources case-insensitively

- **WHEN** the user types a query that matches one source's location in a different letter case
- **THEN** only that source MUST remain listed

#### Scenario: No matching source shows an empty state

- **WHEN** the user types a query that matches no source
- **THEN** Settings → Sources MUST show an empty state naming the query and no source entries

#### Scenario: Clearing the search restores the list

- **WHEN** a query is active and the user clears the search field
- **THEN** every source MUST be listed again

#### Scenario: Source search appears above six sources

- **WHEN** Settings → Sources has six or fewer configured sources
- **THEN** the search field MUST NOT be shown and every source MUST be listed
- **WHEN** Settings → Sources has more than six configured sources
- **THEN** the search field MUST be shown
