## REMOVED Requirements

### Requirement: Grouped, ranked browsing

**Reason**: Moved to `session-navigation`.

**Migration**: Move test annotations to `session-navigation/ranked-search-within-sections`, `session-navigation/clear-and-refresh`, `session-navigation/scattered-metadata-letters`, and `session-navigation/name-match-outranks-metadata-match`.

### Requirement: Accurate match highlighting

**Reason**: Moved to `session-navigation`.

**Migration**: Move test annotations to `session-navigation/metadata-only-match`.

### Requirement: Complete installed Marketplace skill browsing

**Reason**: Moved to `session-navigation`.

**Migration**: Move test annotations to `session-navigation/installed-skill-definition-and-supporting-files-remain-browsable`.

### Requirement: Read-only exact Marketplace skill inspection

**Reason**: Moved to `session-navigation`.

**Migration**: Move test annotations to `session-navigation/inspect-an-exact-skill-before-installation-without-writes`.

## MODIFIED Requirements

### Requirement: Safe search input

The TUI SHALL route typed and bracketed-pasted query text only to the focused search input. A paste SHALL NOT dispatch keyboard shortcuts or affect a modal or the resize-required view. Local filtering and debounced remote Marketplace search SHALL follow `session-navigation`.

#### Scenario: Paste in search

- **WHEN** a user pastes text containing action shortcut characters into focused search
- **THEN** the list filters and the remote search debounces without invoking those actions

#### Scenario: Paste outside search

- **WHEN** a modal is focused or the terminal is below 50×12
- **THEN** pasted search text neither changes search state nor triggers actions

### Requirement: Visible applied filter

The TUI SHALL clear an applied filter when Esc is pressed in the resource list. Query/count visibility and resuming the current query SHALL follow `session-navigation`.

#### Scenario: Confirmed filter

- **WHEN** a user confirms a query with Enter and then presses Esc in the list
- **THEN** the query and match count remain visible until Esc, which restores the unfiltered list

#### Scenario: Refine a filter

- **WHEN** a user starts search while a filter is applied
- **THEN** the search input opens with the current query and the filter is unchanged

### Requirement: Single status-grouped resource list

The TUI SHALL open directly into one resource list with no top-level tab bar or alternative scope or registry view. It SHALL support selectable Source and Kind grouping without reserving a tab line. Removing the tab bar SHALL give its line to resource content without exceeding supported terminal dimensions. Surface-neutral grouping and list behavior SHALL follow `session-navigation`.

#### Scenario: Single status list at minimum size

- **WHEN** the TUI displays configured and available resources with default preferences at 50 columns by 12 rows, folds a status section, applies and clears a search, and accepts a reload
- **THEN** only status sections are displayed without a tab bar, the list has one more content line than a layout reserving a tab line, and folding, search ranking, and stable selection remain usable within the terminal bounds
