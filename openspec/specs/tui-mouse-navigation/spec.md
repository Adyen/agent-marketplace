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

### Requirement: Right-hand text selection coexists with mouse scrolling

The TUI SHALL highlight unmodified left-button drags within the right-hand details or file-preview pane and copy the selected displayed text on release through the terminal clipboard protocol. It SHALL preserve complete Unicode characters and exclude styling, terminal control sequences, neighboring panes, and layout padding from copied text. A click without a drag SHALL NOT copy. Selection SHALL NOT begin in the resource list, bundle tree, footer, or an overlay other than the errors body. Existing wheel routing and frame pacing SHALL remain unchanged. Scrolling, resize, resource or file changes, and replacement preview content SHALL clear stale selection. Out-of-pane drags that began inside a selectable pane SHALL be clipped to that pane. Escape SHALL clear selection before its normal action; Ctrl+C SHALL retain hard-exit behavior.

#### Scenario: Forward and backward drags copy displayed Unicode text

- **WHEN** the user drags forward or backward across one or multiple displayed lines in the right-hand details or file-preview pane
- **THEN** the selected text MUST be visibly highlighted and copied on release with complete Unicode characters, without styling or terminal controls

#### Scenario: Gestures outside selectable text do not copy

- **WHEN** a drag starts in the resource list, bundle tree, footer, or an overlay other than the errors body, or the user clicks without dragging
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

### Requirement: Full-view text selection follows the displayed preview

Full-view bundle labels SHALL remain within the tree's allocated columns at supported terminal sizes. Unmodified left-button drags in the right-hand preview SHALL highlight and copy exactly the displayed Unicode text through the terminal clipboard protocol, independent of label length.

#### Scenario: Long bundle labels do not displace selectable preview text

- **WHEN** a user opens full view with a bundle label longer than the tree width at a supported narrow terminal size and drags across displayed Unicode text in the right-hand preview
- **THEN** the tree MUST stay within its allocated columns and the drag MUST highlight and copy exactly that displayed preview text without including tree text or padding

### Requirement: Marketplace full-view scrolling preserves selection boundaries

Marketplace full-view browsing SHALL retain pointer-targeted wheel routing, selection cancellation on scrolling, resize and content replacement, and the existing refusal to begin clipboard selection in the tree, footer, or overlay.

#### Scenario: Marketplace selection and scrolling preserve clipboard boundaries

- **WHEN** a user selects Marketplace preview text, scrolls the tree or preview, resizes, changes the preview, or starts a drag in the tree, footer, or overlay
- **THEN** existing wheel routing MUST remain intact, stale selection MUST clear, and gestures starting outside the selectable preview MUST NOT write to the clipboard

### Requirement: Errors-screen text selection follows displayed diagnostics

The TUI SHALL highlight unmodified left-button drags that start within the displayed errors body and copy the selected displayed text on release through the terminal clipboard protocol. Selection SHALL cover the errors body while excluding the footer. Copied text MUST preserve complete Unicode characters across wrapped lines and exclude styling, terminal control sequences, layout padding, and the footer. A click without a drag or a gesture that starts outside displayed error text MUST NOT write to the clipboard. Scrolling, terminal resize, diagnostic replacement, or closing the errors screen SHALL clear stale selection. Escape SHALL clear selection before its existing errors-screen action; `e` SHALL close the errors screen, and Ctrl+C SHALL quit.

#### Scenario: Forward and backward drags copy wrapped Unicode error text

- **WHEN** the user presses `e` and drags forward or backward across wrapped error text containing Unicode characters
- **THEN** the selected text MUST be visibly highlighted and copied on release with complete Unicode characters from the displayed errors body only

#### Scenario: Error-screen gestures outside displayed text do not copy

- **WHEN** the user clicks without dragging, starts a gesture outside displayed error text, or starts in the footer while the errors screen is open
- **THEN** the TUI MUST NOT select text or write to the clipboard

#### Scenario: Scrolling and view changes cancel error-screen selection and preserve exits

- **WHEN** an error-screen selection is active and the user scrolls, the terminal resizes, diagnostics change, or the errors screen closes
- **THEN** stale selection MUST clear; Escape MUST clear selection before its existing action, `e` MUST close the errors screen, and Ctrl+C MUST quit

#### Scenario: Terminal drag transport copies selected error text

- **WHEN** the running TUI receives encoded terminal drag and release input within displayed error text
- **THEN** its terminal output MUST show the selected text highlight and emit a clipboard write containing exactly the selected displayed error text
- **AND** an ignored gesture MUST emit no clipboard write
