## ADDED Requirements

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
