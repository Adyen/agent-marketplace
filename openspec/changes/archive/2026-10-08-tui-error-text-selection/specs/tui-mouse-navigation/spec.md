## MODIFIED Requirements

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

## ADDED Requirements

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
