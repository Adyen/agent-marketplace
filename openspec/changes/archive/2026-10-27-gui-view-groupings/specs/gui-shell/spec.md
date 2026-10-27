## ADDED Requirements

### Requirement: Selectable GUI view grouping

The GUI SHALL offer a toolbar View menu with Group by choices Status, Source, and Kind and Then by choices None, Kind, and Source excluding the primary axis. A choice SHALL apply immediately; choosing a primary axis equal to the current secondary axis SHALL reset the secondary axis to None. Section identity, labels, ordering, nesting, the pinned Needs attention section, leaf ordering, and search behavior SHALL follow the grouping requirements of `tui-navigation`. Folds SHALL be session-only and kept separately for each grouping combination. Changing grouping SHALL keep the selected resource when it is visible, otherwise select its deepest visible enclosing section, otherwise the first item. The sidebar SHALL list the top-level sections of the current grouping. The GUI SHALL persist the choice in GUI-local storage, restore it on the next launch, and use Status and None when the stored value is absent, malformed, or invalid, or when storage is unavailable. Choosing a grouping SHALL NOT issue a local API request or write the TUI view preference file, authored configuration, or acquisition locks.

#### Scenario: Every valid grouping combination retains distinct resources

- **WHEN** the user selects each valid Group by and Then by combination over resources with mixed kinds, sources whose labels are equal but whose refs or subpaths differ, unknown kinds, and attention statuses
- **THEN** every resource SHALL appear once, sections SHALL nest and order by the selected axes with Needs attention pinned first outside Status grouping, empty sections SHALL be absent, the sidebar SHALL list the top-level sections, and the primary axis SHALL NOT be offered as Then by

#### Scenario: Grouping changes preserve folds and selection

- **WHEN** a resource is selected and the user switches to a grouping whose enclosing section is folded, returns to an earlier grouping, and applies and clears a search
- **THEN** the selection SHALL follow the resource or its deepest visible enclosing section, and each grouping combination SHALL restore its own folds

#### Scenario: Grouping choice persists locally without workspace writes

- **WHEN** the user changes the grouping and the GUI restarts, or the stored choice is malformed, invalid, or unavailable
- **THEN** the GUI SHALL restore the stored choice or use Status and None, and changing the grouping SHALL issue no local API request
