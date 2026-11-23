## ADDED Requirements

### Requirement: Named local source labels

A sidebar directory or Git source with a non-empty configured name SHALL be labeled by that name in the sidebar and the source browse header. A directory or Git source without a configured name SHALL keep its path label: the last two path segments for a directory, and the host and repository name for a Git source. The full location SHALL stay available in the row's tooltip and accessible name.

#### Scenario: Named local source shows its configured name

- **WHEN** a directory or Git source is declared with a non-empty name
- **THEN** the sidebar row and the source browse header MUST show that name
- **AND** the row's tooltip and accessible name MUST still give the full location

#### Scenario: Unnamed local source keeps its path label

- **WHEN** a directory or Git source is declared without a name
- **THEN** the sidebar row MUST show its path label, the last two path segments for a directory or the host and repository name for a Git source
