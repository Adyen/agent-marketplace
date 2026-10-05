## Purpose

An interactive client is the TUI or the desktop GUI acting on the user's behalf. This capability defines surface-neutral resource browsing, search, highlighting, inspection, and status-grouped list behavior.

## ADDED Requirements

### Requirement: Grouped, ranked browsing

An interactive client SHALL match a query fuzzily against Resource names and by case-insensitive word prefix against Resource metadata, where every query term must begin a metadata word. It SHALL keep Resource sections in their existing order, hide sections without matches, rank name matches above metadata-only matches and then by score within each leaf section with deterministic name and identity ties, and restore alphabetical ordering and saved folds when search clears. Searchable metadata SHALL have deterministic key order. When the user changes the query, the client SHALL select the best-ranked match across all sections. Accepted reloads SHALL restore stable row or section selection and preserve the selected Resource's viewport position; disabling a row SHALL select the next row below its previous position.

#### Scenario: Ranked search within sections

- **WHEN** a fuzzy query matches several Resources at different strengths or matches metadata with reordered keys
- **THEN** Resources display by descending relevance within each unchanged section, with deterministic ties

#### Scenario: Clear and refresh

- **WHEN** search clears or remote results arrive during an active query
- **THEN** the list preserves the relevant selection and saved folds, with alphabetical order restored after clear

#### Scenario: Scattered metadata letters

- **WHEN** a query's letters appear in order across a Resource's description but no metadata word begins with the query and its name does not fuzzily match
- **THEN** the Resource is not a match

#### Scenario: Name match outranks metadata match

- **WHEN** a query matches one Resource only by a metadata word and another Resource by name, in the same or a later section
- **THEN** within a shared section the name match displays first, and the name match becomes the selection

### Requirement: Accurate match highlighting

An interactive client SHALL highlight only matched name characters after sanitization; a match solely in metadata SHALL NOT highlight unrelated name characters.

#### Scenario: Metadata-only match

- **WHEN** a query matches a Resource's metadata but no part of its sanitized name
- **THEN** the Resource remains a match without highlighted name characters

### Requirement: Single status-grouped resource list

An interactive client SHALL show one resource list grouped by status by default, with sections in the order Required, Enabled, Pending sync, Stale, Broken, Available, Malformed, omitting empty sections and retaining alphabetical resource-name ordering when unfiltered. Folding, search ranking, and accepted-reload selection anchoring SHALL remain available.

#### Scenario: Status-grouped resource order and filtering

- **WHEN** a list contains resources in multiple statuses and the user filters, folds, and clears a section
- **THEN** the list SHALL retain the specified section order, omit empty sections, restore alphabetical order when unfiltered, and preserve the selection and folds

### Requirement: Search input filtering

An interactive client SHALL filter local results immediately as the query changes and SHALL debounce remote Marketplace search by approximately 250 milliseconds, cancelling superseded requests.

#### Scenario: Local search applies immediately

- **WHEN** a user changes a search query that matches local results
- **THEN** the local list SHALL be filtered immediately without waiting for remote results

#### Scenario: Remote Marketplace search is debounced

- **WHEN** a user changes a query while Marketplace results are available
- **THEN** remote Marketplace search SHALL be debounced by approximately 250 milliseconds
- **AND** a superseded remote request SHALL be cancelled

### Requirement: Visible applied filter

After a query is confirmed, an interactive client SHALL keep displaying the query and its local match count until the filter clears. Starting search SHALL resume editing the current query instead of discarding it.

#### Scenario: Applied query and count remain visible

- **WHEN** a user confirms a query
- **THEN** the query and its local match count SHALL remain visible until the filter is cleared

#### Scenario: Search resumes the current query

- **WHEN** a user starts search while a filter is applied
- **THEN** the search input SHALL open with the current query and the filter SHALL be unchanged

### Requirement: Complete installed Marketplace skill browsing

An interactive client SHALL show and initially preview `SKILL.md` alongside verified supporting files when opening an installed Marketplace skill, including skills whose supporting files are staged separately from their canonical definition. Missing or invalid verified content SHALL produce a visible error without exposing cache implementation files.

#### Scenario: Installed skill definition and supporting files remain browsable

- **WHEN** a user opens an installed Marketplace skill whose verified supporting files use a staged cache directory, or whose verified content is missing or invalid
- **THEN** valid content MUST show and initially preview `SKILL.md` with the supporting files still browsable, while missing or invalid content MUST fail visibly without exposing cache implementation files

### Requirement: Read-only exact Marketplace skill inspection

Opening an uninstalled Marketplace skill SHALL asynchronously load its exact Resource revision's verified inspection projection and primary file. Browsing SHALL NOT install the skill, edit configuration or acquisition locks, or synchronize materialized files. Unavailable, corrupt, or unsupported inspection content SHALL produce an actionable state. A superseded inspection response SHALL NOT replace another Resource's detail.

#### Scenario: Inspect an exact skill before installation without writes

- **WHEN** a user opens an uninstalled Marketplace skill and browses its verified inspection files, or inspection content is unavailable, corrupt, unsupported, or superseded by another Resource
- **THEN** available inspection content MUST show the exact Resource revision's tree and primary file without installation, configuration, acquisition lock, or materialization writes, failures MUST be actionable, and superseded responses MUST NOT replace the current Resource
