## ADDED Requirements

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
