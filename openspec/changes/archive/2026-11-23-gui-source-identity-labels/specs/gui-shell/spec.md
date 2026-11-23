## ADDED Requirements

### Requirement: Source identity labels match the sidebar

When a resource row belongs to a configured source, the source filter chip, the source browse header title, and the inspector's Registry value SHALL show the same label as that source's sidebar row. The source browse header SHALL show its location shortened to the last two path segments, with the full location in its title tooltip.

#### Scenario: Source identity labels match the sidebar

- **WHEN** a resource row belongs to a configured source named "Team" and the user views the row in the inspector
- **THEN** the inspector's Registry value MUST show "Team"
- **AND** the source browse header title for that source MUST show "Team"
