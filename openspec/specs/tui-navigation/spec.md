# tui-navigation Specification

## Purpose
Defines keyboard browsing, deterministic search, selection continuity, and reachable help for the resource-focused terminal interface.

## Requirements

### Requirement: Grouped, ranked browsing
The TUI SHALL match a query fuzzily against resource names and by case-insensitive word prefix against resource metadata, where every query term must begin a metadata word. It SHALL keep resource sections in their existing order, hide sections without matches, rank name matches above metadata-only matches and then by score within each leaf section with deterministic name and identity ties, and restore alphabetical ordering and saved folds when search clears. Searchable metadata SHALL have deterministic key order. When the user changes the query, the TUI SHALL select the best-ranked match across all sections. Accepted reloads SHALL restore stable row or section selection and preserve the selected resource's viewport position; disabling a row SHALL select the next row below its previous position.

#### Scenario: Ranked search within sections
- **WHEN** a fuzzy query matches several resources at different strengths or matches metadata with reordered keys
- **THEN** resources display by descending relevance within each unchanged section, with deterministic ties

#### Scenario: Clear and refresh
- **WHEN** search clears or remote results arrive during an active query
- **THEN** the list preserves the relevant selection and saved folds, with alphabetical order restored after clear

#### Scenario: Scattered metadata letters
- **WHEN** a query's letters appear in order across a resource's description but no metadata word begins with the query and its name does not fuzzily match
- **THEN** the resource is not a match

#### Scenario: Name match outranks metadata match
- **WHEN** a query matches one resource only by a metadata word and another resource by name, in the same or a later section
- **THEN** within a shared section the name match displays first, and the name match becomes the selection

### Requirement: Safe search input
The TUI SHALL route typed and bracketed-pasted query text only to the focused search input, filter locally immediately, and debounce remote Marketplace search approximately 250 milliseconds with cancellation of superseded requests. A paste SHALL NOT dispatch keyboard shortcuts or affect a modal or the resize-required view.

#### Scenario: Paste in search
- **WHEN** a user pastes text containing action shortcut characters into focused search
- **THEN** the list filters and the remote search debounces without invoking those actions

#### Scenario: Paste outside search
- **WHEN** a modal is focused or the terminal is below 50×12
- **THEN** pasted search text neither changes search state nor triggers actions

### Requirement: Accurate match highlighting
The TUI SHALL highlight only matched name characters after sanitization; a match solely in metadata SHALL NOT highlight unrelated name characters.

#### Scenario: Metadata-only match
- **WHEN** a query matches a resource's metadata but no part of its sanitized name
- **THEN** the resource remains a match without highlighted name characters

### Requirement: Reachable keyboard help
The TUI SHALL expose every help binding through vertical scrolling at the supported minimum size, preserve the scroll offset across close and reopen, and keep close and hard-quit hints visible.

#### Scenario: Narrow help
- **WHEN** a user opens help in a 50×12 terminal and scrolls to the final group, then closes and reopens it
- **THEN** every binding is reachable and the scroll offset and quit hints remain available

### Requirement: Visible applied filter
After a query is confirmed, the TUI SHALL keep displaying the query and its local match count until the filter clears. Esc in the resource list SHALL clear an applied filter, and starting search SHALL resume editing the current query instead of discarding it.

#### Scenario: Confirmed filter
- **WHEN** a user confirms a query with Enter and then presses Esc in the list
- **THEN** the query and match count remain visible until Esc, which restores the unfiltered list

#### Scenario: Refine a filter
- **WHEN** a user starts search while a filter is applied
- **THEN** the search input opens with the current query and the filter is unchanged

### Requirement: Single status-grouped resource list

The TUI SHALL open directly into one resource list grouped by status, with no top-level tab bar or alternative scope or registry view. It SHALL keep the section order Required, Enabled, Pending sync, Stale, Broken, Available, Malformed, omit empty sections, and retain alphabetical resource-name ordering when unfiltered. Folding, search ranking, and accepted reload selection anchoring SHALL remain available. Removing the tab bar SHALL give its line to resource content without exceeding supported terminal dimensions.

#### Scenario: Single status list at minimum size

- **WHEN** the TUI displays configured and available resources at 50 columns by 12 rows, folds a status section, applies and clears a search, and accepts a reload
- **THEN** only status sections are displayed without a tab bar, the list has one more content line than a layout reserving a tab line, and folding, search ranking, and stable selection remain usable within the terminal bounds

### Requirement: Former tab shortcuts are inactive

The resource list SHALL NOT switch views or change navigation state when the user presses `1`, `2`, `3`, `tab`, or `shift+tab`. Help, footer, and command palette SHALL NOT advertise tab switching. Modal field navigation SHALL retain its native `tab` and `shift+tab` behavior.

#### Scenario: Former tab keys leave list state unchanged

- **WHEN** a user presses the former tab shortcuts while browsing the resource list, then opens help, the command palette, and a multi-field modal
- **THEN** the shortcuts leave list selection, filter, folds, and grouping unchanged, help and palette contain remaining supported actions but no tab switches, and Tab can still move between modal fields

### Requirement: Single-view resource browsing

The single status-grouped resource list SHALL support resource inspection, search, section folding, and scrolling without requiring a view switch.

#### Scenario: Browse inspect search fold and scroll in one view

- **WHEN** a user opens a list longer than the terminal, inspects a resource, searches for it, clears the query, folds and expands its section, and scrolls to a resource below the initial viewport
- **THEN** the resource detail, search result, restored section, and scrolled resource remain reachable in the same status-grouped view

### Requirement: Complete installed Marketplace skill browsing

The TUI SHALL show and initially preview `SKILL.md` alongside verified supporting files when opening an installed Marketplace skill, including skills whose supporting files are staged separately from their canonical definition. Missing or invalid verified content SHALL produce a visible error without exposing cache implementation files.

#### Scenario: Installed skill definition and supporting files remain browsable

- **WHEN** a user opens an installed Marketplace skill whose verified supporting files use a staged cache directory, or whose verified content is missing or invalid
- **THEN** valid content MUST show and initially preview `SKILL.md` with the supporting files still browsable, while missing or invalid content MUST fail visibly without exposing cache implementation files

### Requirement: Read-only exact Marketplace skill inspection

Opening an uninstalled Marketplace skill SHALL asynchronously load its exact resource revision's verified inspection projection and primary file. Browsing SHALL NOT install the skill, edit configuration or acquisition locks, or synchronize materialized files. Unavailable, corrupt, or unsupported inspection content SHALL produce an actionable state. A superseded inspection response SHALL NOT replace another resource's detail.

#### Scenario: Inspect an exact skill before installation without writes

- **WHEN** a user opens an uninstalled Marketplace skill and browses its verified inspection files, or inspection content is unavailable, corrupt, unsupported, or superseded by another resource
- **THEN** available inspection content MUST show the exact resource revision's tree and primary file without installation, configuration, acquisition lock, or materialization writes, failures MUST be actionable, and superseded responses MUST NOT replace the current resource
