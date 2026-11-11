## ADDED Requirements

### Requirement: Grouped subpath sources

In the sidebar and in Settings → Sources, sources in the same layer (user or organization) that share a kind and location SHALL render under one parent entry when two or more of them exist, indented beneath it and each showing its subpath. A source alone at its kind and location SHALL render as a single entry without a parent. Grouping SHALL NOT change authored configuration.

#### Scenario: Sources sharing a location are grouped

- **WHEN** the configuration declares two Git sources with the same location and different subpaths, and one other source
- **THEN** the sidebar and Settings → Sources MUST show one parent entry with the two sources indented beneath it, each showing its subpath
- **AND** the other source MUST render as a single entry without a parent

### Requirement: Foldable source groups

A source group parent SHALL be a named control exposing its expanded state that folds and unfolds its sources by pointer and by keyboard. Groups SHALL start unfolded, and folds SHALL be session-only and kept separately for the sidebar and Settings → Sources.

#### Scenario: Group folds by pointer and keyboard

- **WHEN** the user clicks a group parent, then focuses it and presses Enter or Space
- **THEN** the click MUST fold the group and hide its sources, the key press MUST unfold it again, and the parent's expanded state MUST match each time

### Requirement: Sidebar sources open their Settings entry

Selecting a sidebar source row SHALL open Settings on the Sources tab with that source scrolled into view, focused, and visibly highlighted. When the source is inside a folded group in Settings → Sources, the group SHALL unfold; when an active source search hides it, the search SHALL clear. The sidebar ＋ next to SOURCES SHALL open Settings on the Sources tab with focus in the Add source form's location field.

#### Scenario: Selecting a sidebar source reveals it in Settings

- **WHEN** the user selects a sidebar source row
- **THEN** Settings MUST open on the Sources tab with that source's entry in view, focused, and highlighted

#### Scenario: Revealing a source unfolds its folded group

- **WHEN** the source's group is folded in Settings → Sources and the user selects that source in the sidebar
- **THEN** the group MUST unfold and the source's entry MUST be in view and highlighted

#### Scenario: Sidebar add control focuses the Add source form

- **WHEN** the user activates the sidebar ＋ next to SOURCES
- **THEN** Settings MUST open on the Sources tab with focus in the Add source form's location field

### Requirement: Source search in Settings

Settings → Sources SHALL offer a search field that filters user and organization sources to those whose label, location, or subpath contains the query, ignoring case. A group SHALL stay while any of its sources matches and SHALL show only matching sources. When no source matches, Settings → Sources SHALL show an empty state naming the query. Clearing the field SHALL restore the full list. Searching SHALL NOT issue a local API request.

#### Scenario: Search filters sources case-insensitively

- **WHEN** the user types a query that matches one source's location in a different letter case
- **THEN** only that source MUST remain listed

#### Scenario: No matching source shows an empty state

- **WHEN** the user types a query that matches no source
- **THEN** Settings → Sources MUST show an empty state naming the query and no source entries

#### Scenario: Clearing the search restores the list

- **WHEN** a query is active and the user clears the search field
- **THEN** every source MUST be listed again
