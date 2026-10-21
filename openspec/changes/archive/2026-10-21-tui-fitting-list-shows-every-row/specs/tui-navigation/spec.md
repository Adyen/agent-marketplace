## ADDED Requirements

### Requirement: A list that fits shows every row

When every row of the resource list fits within the list viewport, the TUI
SHALL show the list from its first row. Keeping a selection on its previous
screen line across a reload SHALL apply only while the list overflows the
viewport, so rows are never hidden above the viewport while the list has room
for them.

#### Scenario: Reload with room for every row shows the whole list

- **WHEN** a section header lower in the list is selected and a reload inserts
  rows above it, and every row still fits within the viewport
- **THEN** the list MUST start at its first row, showing every section, and the
  same header MUST remain selected
