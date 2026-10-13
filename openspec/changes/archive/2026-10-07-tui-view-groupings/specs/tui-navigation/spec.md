## MODIFIED Requirements

### Requirement: Single status-grouped resource list

The TUI SHALL open into one resource list grouped by Status by default, without a top-level tab bar. It SHALL support selectable Source and Kind grouping without reserving a tab line. The default SHALL keep Required, Enabled, Pending sync, Stale, Broken, Available, Malformed order, omit empty sections, and retain alphabetical resource-name ordering when unfiltered. Folding, search ranking, accepted reload selection anchoring, and supported terminal dimensions SHALL remain available.

#### Scenario: Single status list at minimum size

- **WHEN** the TUI displays configured and available resources with default preferences at 50 columns by 12 rows, folds a status section, applies and clears a search, and accepts a reload
- **THEN** only status sections are displayed without a tab bar, the list has one more content line than a layout reserving a tab line, and folding, search ranking, and stable selection remain usable within the terminal bounds

### Requirement: Single-view resource browsing

The resource list SHALL support resource inspection, search, section folding, and scrolling within the selected grouping without requiring tabs or a view switch.

#### Scenario: Browse inspect search fold and scroll in one view

- **WHEN** a user opens a list longer than the terminal, inspects a resource, searches for it, clears the query, folds and expands its section, and scrolls to a resource below the initial viewport
- **THEN** the resource detail, search result, restored section, and scrolled resource remain reachable in the selected grouping

## ADDED Requirements

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
