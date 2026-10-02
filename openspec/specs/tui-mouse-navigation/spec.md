# tui-mouse-navigation Specification

## Purpose
Define how the terminal UI routes mouse-wheel input among its resource list, bundle browser, and scrollable overlays while preserving keyboard navigation and restoring terminal mouse modes on exit.

## Requirements

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

### Requirement: Resource-list wheel bursts preserve ordered selection

The TUI SHALL apply every accepted vertical resource-list wheel event in its original order, including events at a list boundary. Frame pacing SHALL NOT change scroll distance, modifier handling, ignored mouse events, or selection through wrapped resource rows.

#### Scenario: Rapid wheel reversals preserve selection

- **WHEN** a burst of vertical wheel events changes direction at the first or last list entry or traverses wrapped resource rows
- **THEN** the final selection MUST equal applying every event in order under the existing one-entry navigation rules
- **AND** ignored mouse events and Shift-modified vertical input MUST retain their existing behavior

### Requirement: Resource-list wheel presentation is frame-paced

During a resource-list wheel-only burst, the TUI SHALL refresh the resource-list presentation at most once per 60 Hz frame interval while applying navigation input without waiting for that refresh. A pending final presentation update SHALL run after the last event without requiring more user input.

#### Scenario: Final selection appears after a wheel burst

- **WHEN** several resource-list wheel events arrive within one frame interval and then input stops
- **THEN** the TUI MUST present the newest selected resource after the pending frame update
- **AND** it MUST NOT construct a separate presentation for each intermediate selection

### Requirement: Other input and state changes bypass wheel presentation pacing

Keyboard input, terminal resize, asynchronous state updates, and pane changes SHALL refresh from the current TUI state immediately rather than waiting behind a resource-list wheel presentation timer. Pending presentation timers SHALL NOT restore an obsolete pane or selection, and commands produced by state updates SHALL remain executable.

#### Scenario: Non-wheel updates supersede pending frames

- **WHEN** a key, resize, snapshot, or pane change follows resource-list wheel input before its pending frame update
- **THEN** that update MUST use the current selection and display the resulting state immediately
- **AND** a stale presentation timer MUST NOT revert that state or suppress the update's commands

### Requirement: Wheel bursts retain their complete terminal outcome

Resource-list presentation pacing SHALL preserve the complete input outcome through the normal terminal input path. Verification of wheel bursts SHALL assert an independently expected final selection and retain all accepted events rather than achieving lower latency by discarding input.

#### Scenario: Terminal wheel burst reaches the expected resource

- **WHEN** the active resource list receives a bounded burst of encoded vertical wheel input followed by a direction reversal
- **THEN** the terminal MUST show the independently expected selected resource and its details
- **AND** the completed burst MUST preserve the accepted event count and order
