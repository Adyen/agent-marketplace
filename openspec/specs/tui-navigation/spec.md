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

The TUI SHALL open into one resource list grouped by Status by default, without a top-level tab bar. It SHALL support selectable Source and Kind grouping without reserving a tab line. The default SHALL keep Required, Enabled, Pending sync, Stale, Broken, Available, Malformed order, omit empty sections, and retain alphabetical resource-name ordering when unfiltered. Folding, search ranking, accepted reload selection anchoring, and supported terminal dimensions SHALL remain available.

#### Scenario: Single status list at minimum size

- **WHEN** the TUI displays configured and available resources with default preferences at 50 columns by 12 rows, folds a status section, applies and clears a search, and accepts a reload
- **THEN** only status sections are displayed without a tab bar, the list has one more content line than a layout reserving a tab line, and folding, search ranking, and stable selection remain usable within the terminal bounds

### Requirement: Former tab shortcuts are inactive

The resource list SHALL NOT switch views or change navigation state when the user presses `1`, `2`, `3`, `tab`, or `shift+tab`. Help, footer, and command palette SHALL NOT advertise tab switching. Modal field navigation SHALL retain its native `tab` and `shift+tab` behavior.

#### Scenario: Former tab keys leave list state unchanged

- **WHEN** a user presses the former tab shortcuts while browsing the resource list, then opens help, the command palette, and a multi-field modal
- **THEN** the shortcuts leave list selection, filter, folds, and grouping unchanged, help and palette contain remaining supported actions but no tab switches, and Tab can still move between modal fields

### Requirement: Single-view resource browsing

The resource list SHALL support resource inspection, search, section folding, and scrolling within the selected grouping without requiring tabs or a view switch.

#### Scenario: Browse inspect search fold and scroll in one view

- **WHEN** a user opens a list longer than the terminal, inspects a resource, searches for it, clears the query, folds and expands its section, and scrolls to a resource below the initial viewport
- **THEN** the resource detail, search result, restored section, and scrolled resource remain reachable in the selected grouping

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

### Requirement: Command palette typing remains distinct from navigation

The command palette SHALL append ordinary typed characters to its focused query, including `j` and `k`. Only the up and down arrow keys SHALL move the palette's match cursor.

#### Scenario: j and k remain query text

- **WHEN** the command palette has multiple matching actions, its match cursor is not at the first action, and the user types `j` or `k`
- **THEN** each character SHALL be appended to the palette query without dispatching an action, while up and down arrows continue to move the match cursor

### Requirement: Selectable resource groupings

The primary grouping SHALL be Status, Source, or Kind. The secondary grouping SHALL be None, Kind, or Source, excluding the primary axis; Status SHALL NOT be secondary. Source identity SHALL distinguish normalized URL, ref, and full normalized configured registry subpath. Source sections SHALL use registry display name or URL-derived repository name with the full subpath, or `(local)` without registry metadata; they SHALL sort by label then identity. Kind sections SHALL order Skills, Commands, Subagents, Hooks, MCP servers, Collections, Rules, then unknown kinds alphabetically. Leaf rows SHALL sort alphabetically without a filter and by existing search relevance with deterministic ties when filtered; empty sections SHALL be omitted.

#### Scenario: All grouping combinations retain distinct resources

- **WHEN** the user selects every valid primary and secondary combination over mixed kinds and sources, including equal source labels with different refs or normalized subpaths and unknown kinds
- **THEN** every resource SHALL appear once, nesting and section order SHALL follow the selected axes, equal labels SHALL remain distinct and deterministic, and empty sections SHALL be absent

### Requirement: Pinned attention outside Status grouping

When the primary grouping is not Status, Pending sync, Stale, Broken, and Malformed rows SHALL move into one top Needs attention section, ordered by the Status bucket order then name. Required and Available rows SHALL remain in their normal groups with existing status glyphs. The attention section SHALL NOT duplicate resources.

#### Scenario: Attention rows move without duplication

- **WHEN** Source or Kind is primary and the list contains attention statuses alongside Required and Available resources
- **THEN** attention rows SHALL appear only in the top Needs attention section in status then name order, other rows SHALL remain grouped, and an empty attention section SHALL be omitted

### Requirement: Grouping-aware search folds and selection

Search metadata SHALL include resource kind and the displayed source label under every grouping, retaining existing name-versus-metadata ranking. Search SHALL hide sections without matches and expose matching rows while preserving session fold state separately for every grouping combination. Switching grouping SHALL retain the selected resource when visible, otherwise select its containing visible section, otherwise the first item. Clearing search SHALL restore saved folds; no matches SHALL leave a valid empty selection state.

#### Scenario: Kind and source searches work across groupings

- **WHEN** a query matches only a resource kind or source label under any supported grouping, or has no matches
- **THEN** matching resources SHALL remain reachable with existing metadata-only ranking and highlighting rules, nonmatching sections SHALL be hidden, and no matches SHALL leave an empty list without an invalid cursor

#### Scenario: Grouping changes preserve folds and resource selection

- **WHEN** a resource is selected, the user changes grouping to one where its parent is folded, then returns to a previously folded grouping and applies and clears search
- **THEN** selection SHALL follow the resource or its visible enclosing section, each grouping combination SHALL restore its own saved folds, and search SHALL NOT overwrite them

### Requirement: Query-local category folding

The TUI SHALL initially expand matching categories when a nonempty query begins or changes. After the query is confirmed, all existing category fold controls SHALL operate on the filtered list without changing the query, match count, or saved browsing folds. Enter on a category header SHALL toggle its expansion and keep that header selected in the resource list. Search folds SHALL survive accepted reloads and remote results for the unchanged query, including when the selected header is the first item. Reopening the same query SHALL preserve its folds. Clearing the query SHALL discard search folds and restore browsing folds. An empty or no-match result SHALL remain safe to navigate and fold.

#### Scenario: Fold a category in applied search

- **WHEN** a user confirms a fuzzy query, selects a matching category header, and collapses then expands it with Enter or another existing fold control
- **THEN** the category's matching children SHALL hide then reappear, the header SHALL remain selected for header-local controls, the query and match count SHALL remain unchanged, and saved browsing folds SHALL not change

#### Scenario: Change query reveals previously folded matches

- **WHEN** a user starts search with browsing categories folded, folds a category in the applied search, reopens that query, then changes it or clears it
- **THEN** initial search SHALL reveal matching children, reopening the unchanged query SHALL retain search folds, a changed query SHALL reveal matching children again, and clearing SHALL restore the pre-search browsing folds even after expanding all search categories

#### Scenario: Refresh retains search folds and header selection

- **WHEN** accepted remote results or a reload rebuild an applied search while a folded category header is selected, including the first header
- **THEN** that category SHALL remain folded and selected with its matching children hidden, while other expanded categories SHALL display their matching results

#### Scenario: Empty search results tolerate folding

- **WHEN** a query has no matches and the user invokes category fold controls, changes the query to one with matches, or clears it
- **THEN** no invalid selection or resource action SHALL occur, matching categories SHALL appear expanded for the changed query, and clearing SHALL restore the saved browsing folds
