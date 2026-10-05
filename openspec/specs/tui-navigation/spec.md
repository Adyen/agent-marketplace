# tui-navigation Specification

## Purpose
Defines keyboard browsing, deterministic search, selection continuity, and reachable help for the resource-focused terminal interface.

## Requirements

### Requirement: Safe search input

The TUI SHALL route typed and bracketed-pasted query text only to the focused search input. A paste SHALL NOT dispatch keyboard shortcuts or affect a modal or the resize-required view. Local filtering and debounced remote Marketplace search SHALL follow `session-navigation`.

#### Scenario: Paste in search

- **WHEN** a user pastes text containing action shortcut characters into focused search
- **THEN** the list filters and the remote search debounces without invoking those actions

#### Scenario: Paste outside search

- **WHEN** a modal is focused or the terminal is below 50×12
- **THEN** pasted search text neither changes search state nor triggers actions

### Requirement: Reachable keyboard help
The TUI SHALL expose every help binding through vertical scrolling at the supported minimum size, preserve the scroll offset across close and reopen, and keep close and hard-quit hints visible.

#### Scenario: Narrow help
- **WHEN** a user opens help in a 50×12 terminal and scrolls to the final group, then closes and reopens it
- **THEN** every binding is reachable and the scroll offset and quit hints remain available

### Requirement: Visible applied filter

The TUI SHALL clear an applied filter when Esc is pressed in the resource list. Query/count visibility and resuming the current query SHALL follow `session-navigation`.

#### Scenario: Confirmed filter

- **WHEN** a user confirms a query with Enter and then presses Esc in the list
- **THEN** the query and match count remain visible until Esc, which restores the unfiltered list

#### Scenario: Refine a filter

- **WHEN** a user starts search while a filter is applied
- **THEN** the search input opens with the current query and the filter is unchanged

### Requirement: Single status-grouped resource list

The TUI SHALL open directly into one resource list with no top-level tab bar or alternative scope or registry view. Removing the tab bar SHALL give its line to resource content without exceeding supported terminal dimensions. Surface-neutral grouping and list behavior SHALL follow `session-navigation`.

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

### Requirement: Command palette typing remains distinct from navigation

The command palette SHALL append ordinary typed characters to its focused query, including `j` and `k`. Only the up and down arrow keys SHALL move the palette's match cursor.

#### Scenario: j and k remain query text

- **WHEN** the command palette has multiple matching actions, its match cursor is not at the first action, and the user types `j` or `k`
- **THEN** each character SHALL be appended to the palette query without dispatching an action, while up and down arrows continue to move the match cursor
