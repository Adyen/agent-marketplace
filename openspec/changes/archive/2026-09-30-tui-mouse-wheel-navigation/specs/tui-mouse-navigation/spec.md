## ADDED Requirements

### Requirement: Vertical wheel input follows the active TUI view

The terminal UI SHALL enable cell-motion mouse reporting while active. A vertical wheel press SHALL move the resource-list selection or the visible bundle-tree selection by one entry per event, or scroll the file preview, help, errors, or Marketplace consent by one line per event. In the split bundle browser, the wheel SHALL target the tree when the pointer is inside its column range and the preview otherwise. Shift-modified vertical wheel events SHALL remain vertical.

#### Scenario: Wheel reporting is enabled

- **WHEN** the TUI enters its alternate screen
- **THEN** it MUST enable terminal mouse reporting for wheel input without enabling all-motion tracking

#### Scenario: List selection moves by one entry

- **WHEN** a vertical wheel event arrives while the resource list is active
- **THEN** the selection MUST move by one entry in the wheel direction within list bounds

#### Scenario: Reversing at the list boundary is immediate

- **WHEN** a wheel-up event reaches the first list entry and a wheel-down event follows
- **THEN** the selection MUST move to the next entry on that wheel-down event

#### Scenario: Bundle tree and preview receive their own wheel input

- **WHEN** a vertical wheel event arrives over the tree or preview in a split bundle browser
- **THEN** wheel input over the tree MUST change its selected entry and request the matching preview
- **AND** wheel input over the preview MUST scroll that preview without changing the tree selection

#### Scenario: Scrollable overlays move by one line

- **WHEN** a vertical wheel event arrives while overflowing help, errors, or Marketplace consent is visible
- **THEN** the active view MUST scroll by one line without changing other views

#### Scenario: Unrelated mouse events are ignored

- **WHEN** the TUI receives a click, release, drag, or horizontal-wheel event
- **THEN** it MUST NOT perform a TUI action
- **AND** Shift-modified vertical wheel events MUST still scroll vertically

#### Scenario: Wheel input below the minimum terminal size is ignored

- **WHEN** the terminal is narrower than 50 columns or shorter than 12 rows
- **THEN** a vertical wheel event MUST NOT change the resource-list selection

### Requirement: Wheel navigation preserves detail state and terminal ownership

Scrolling the detail preview with the wheel SHALL clear its sticky-bottom state so that an asynchronous detail refresh preserves the user's new scroll position. The terminal UI SHALL disable mouse reporting and restore the terminal on clean quit and handled failure paths. Because mouse events are reported to the TUI while active, native drag selection MAY require a terminal-specific modifier.

#### Scenario: Detail refresh retains a wheel scroll

- **WHEN** the user wheels upward in the detail preview before an in-flight detail refresh completes
- **THEN** that refresh MUST retain the resulting viewport offset instead of snapping to the bottom

#### Scenario: Exiting restores terminal mouse modes

- **WHEN** the TUI exits cleanly or through a handled error or interrupt
- **THEN** it MUST disable its mouse modes and restore the alternate screen and cursor state
