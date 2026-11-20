## MODIFIED Requirements

### Requirement: Sidebar sources open their Settings entry

A named Manage source action for each sidebar source SHALL open Settings on the Sources tab with that source scrolled into view, focused, and visibly highlighted. When the source is inside a folded group in Settings, the group SHALL unfold; when an active source search hides it, the search SHALL clear. The sidebar add control SHALL open Settings on the Sources tab with focus in the Add source form's location field. Browsing a source SHALL NOT open Settings.

#### Scenario: Selecting a sidebar source reveals it in Settings

- **WHEN** the user activates Manage source for a sidebar source
- **THEN** Settings MUST open on the Sources tab with that source's entry in view, focused, and highlighted

#### Scenario: Revealing a source unfolds its folded group

- **WHEN** the source's group is folded in Settings and the user activates Manage source for that source
- **THEN** the group MUST unfold and the source's entry MUST be in view and highlighted

#### Scenario: Sidebar add control focuses the Add source form

- **WHEN** the user activates the sidebar add control next to Sources
- **THEN** Settings MUST open on the Sources tab with focus in the Add source form's location field

## ADDED Requirements

### Requirement: Browse resources by source

The GUI SHALL let the user filter Resources in place by one configured source at a time, by pointer or keyboard. It SHALL name the active source and provide a clear control. The filter SHALL constrain both browse and search results without changing session-wide counters, grouping options, or stored configuration. Clearing it SHALL retain any current query.

#### Scenario: Source selection filters resources and clears in place

- **WHEN** the user activates a source in a mixed-source workspace, then clears the source filter with no search query
- **THEN** only resources attributed to that source MUST be shown while the filter is active, it MUST be named and clearable, no Settings dialog MUST open, and clearing MUST restore all resources

#### Scenario: Clearing the source retains the current search

- **WHEN** the user clears the active source filter while a search query is active
- **THEN** the query MUST remain active and its results MUST no longer be limited to that source

#### Scenario: Empty or unavailable source cannot reveal unrelated rows

- **WHEN** the user selects a source that has no attributable resources after loading settles
- **THEN** the GUI MUST retain the active source and its clear control and show an empty view instead of unrelated rows

### Requirement: Progressive Marketplace browse inventory

For eligible Marketplace endpoints the GUI SHALL begin cached-first browse on validated startup configuration and load all pages in the background, independent of search. Browse SHALL obey existing endpoint policy and configuration generation. Browse and search SHALL NOT write authored configuration, acquisition locks, or materialized resources.

#### Scenario: Later Marketplace pages are browsable during and after search

- **WHEN** validated startup configuration declares a Marketplace catalog that takes multiple pages and the user starts and clears a search while a later page is pending with its source filter active
- **THEN** received browse rows MUST be usable before the later page, search MUST show only results matching the query and source, and clearing MUST restore the active source's browse rows including later rows as they are received without a resource-name query

#### Scenario: Later Marketplace page failure leaves an honest partial catalog

- **WHEN** an allowed Marketplace returns an initial page and a later page fails
- **THEN** the GUI MUST retain received rows and only nonduplicate eligible cached fallback, indicate incomplete discovery in Diagnostics independently of search, and MUST NOT treat the partial result as a complete cache on a subsequent launch

#### Scenario: Superseded Marketplace browse cannot replace current results

- **WHEN** the GUI explicitly refreshes or accepts changed endpoint configuration while a browse response is delayed, or quits with a page pending
- **THEN** explicit refresh MUST start fresh browse, obsolete responses MUST NOT replace current results, denied endpoints MUST NOT be contacted, and quitting MUST cancel the outstanding browse
