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
