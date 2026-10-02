## MODIFIED Requirements

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

- **WHEN** the TUI receives a click without a drag, a release without an active selection, a drag beginning outside a selectable right-hand pane, or a horizontal-wheel event
- **THEN** it MUST NOT perform a TUI action
- **AND** Shift-modified vertical wheel events MUST still scroll vertically

#### Scenario: Wheel input below the minimum terminal size is ignored

- **WHEN** the terminal is narrower than 50 columns or shorter than 12 rows
- **THEN** a vertical wheel event MUST NOT change the resource-list selection

### Requirement: Wheel navigation preserves detail state and terminal ownership

Scrolling the detail preview with the wheel SHALL clear its sticky-bottom state so that an asynchronous detail refresh preserves the user's new scroll position. The terminal UI SHALL disable mouse reporting and restore the terminal on clean quit and handled failure paths. The TUI SHALL provide unmodified drag selection in its selectable right-hand details and file-preview panes; native drag selection elsewhere MAY require a terminal-specific modifier.

#### Scenario: Detail refresh retains a wheel scroll

- **WHEN** the user wheels upward in the detail preview before an in-flight detail refresh completes
- **THEN** that refresh MUST retain the resulting viewport offset instead of snapping to the bottom

#### Scenario: Exiting restores terminal mouse modes

- **WHEN** the TUI exits cleanly or through a handled error or interrupt
- **THEN** it MUST disable its mouse modes and restore the alternate screen and cursor state

## ADDED Requirements

### Requirement: Right-hand text selection coexists with mouse scrolling

The TUI SHALL highlight unmodified left-button drags within the right-hand details or file-preview pane and copy the selected displayed text on release through the terminal clipboard protocol. It SHALL preserve complete Unicode characters and exclude styling, terminal control sequences, neighboring panes, and layout padding from copied text. A click without a drag SHALL NOT copy. Selection SHALL NOT begin in the resource list, bundle tree, footer, or an overlay. Existing wheel routing and frame pacing SHALL remain unchanged. Scrolling, resize, resource or file changes, and replacement preview content SHALL clear stale selection. Out-of-pane drags that began inside a selectable pane SHALL be clipped to that pane. Escape SHALL clear selection before its normal action; Ctrl+C SHALL retain hard-exit behavior.

#### Scenario: Forward and backward drags copy displayed Unicode text

- **WHEN** the user drags forward or backward across one or multiple displayed lines in the right-hand details or file-preview pane
- **THEN** the selected text MUST be visibly highlighted and copied on release with complete Unicode characters, without styling or terminal controls

#### Scenario: Gestures outside selectable text do not copy

- **WHEN** a drag starts in the resource list, bundle tree, footer, or an overlay, or the user clicks without dragging
- **THEN** the TUI MUST NOT select text or write to the clipboard

#### Scenario: Scrolling clears selection without changing navigation

- **WHEN** a vertical wheel event arrives while right-hand text is selected
- **THEN** selection MUST clear and the existing pointer-targeted one-entry or one-line navigation MUST occur with resource-list frame pacing unchanged

#### Scenario: Selection remains safe across view changes and exits

- **WHEN** selection is active and the terminal resizes, the selected resource or file changes, preview content is replaced, the drag leaves its pane, Escape is pressed, or Ctrl+C is pressed
- **THEN** resize and content changes MUST clear stale selection, out-of-pane dragging MUST stay clipped to the selectable pane, Escape MUST clear selection before its normal action, and Ctrl+C MUST retain hard-exit behavior

#### Scenario: Terminal gestures deliver only the selected text

- **WHEN** the running TUI receives wheel navigation followed by an encoded right-pane drag and release
- **THEN** its terminal output MUST show the selected text highlight and emit a clipboard write containing exactly the selected displayed text
- **AND** ignored gestures MUST emit no clipboard write
