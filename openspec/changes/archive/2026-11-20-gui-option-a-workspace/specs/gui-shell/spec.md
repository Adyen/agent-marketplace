## ADDED Requirements

### Requirement: Stackable workspace facets

Resources SHALL support one kind and one status facet at a time, intersected with source and search. Kind and status SHALL be independently clearable; the explicit All resources facet reset SHALL clear kind, status and source but retain search. Facet changes SHALL NOT change persisted configuration, grouping preferences or session-wide counters. Needs attention SHALL select stale, broken and malformed rows; Required by org SHALL select rows marked required.

#### Scenario: Workspace facets intersect and clear independently

- **WHEN** a user combines a source, kind or status and a search
- **THEN** only their intersection MUST be shown, clearing one MUST preserve the others, and All resources MUST reset the facets without clearing search.

### Requirement: Context actions for a browsed source

While browsing a current source in Resources, the workspace SHALL show its projected kind, sanitized location and any declared ref/subpath. Source problems SHALL count attributable stale, broken or malformed rows, not unassociated global diagnostics. The source actions SHALL copy the projected location, open only safe absolute HTTP(S) locations in an external browser, and offer removal only for editable user sources via the existing Settings confirmation and operation.

#### Scenario: Source header exposes attributable problems and safe actions

- **WHEN** a source is selected
- **THEN** the source header MUST show available location metadata, any problems count MUST represent only attributable rows and allow filtering to Needs attention, the location MUST be copyable, and only eligible web locations MUST be openable.

#### Scenario: Source shortcut preserves removal confirmation and scope

- **WHEN** a user invokes Remove source from a browsed editable user source
- **THEN** Settings MUST show confirmation for that same source and MUST NOT mutate until explicitly confirmed; organization sources MUST NOT offer removal.
