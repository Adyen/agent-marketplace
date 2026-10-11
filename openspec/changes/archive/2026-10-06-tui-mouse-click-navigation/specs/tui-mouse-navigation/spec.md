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

- **WHEN** the TUI receives a click without a drag outside a resource-list row or bundle-tree entry, a release without an active selection or pending navigation click, a drag beginning outside a selectable right-hand pane, or a horizontal-wheel event
- **THEN** it MUST NOT perform a TUI action
- **AND** Shift-modified vertical wheel events MUST still scroll vertically

#### Scenario: Wheel input below the minimum terminal size is ignored

- **WHEN** the terminal is narrower than 50 columns or shorter than 12 rows
- **THEN** a vertical wheel event MUST NOT change the resource-list selection

## ADDED Requirements

### Requirement: Left clicks navigate the resource list and bundle tree

The TUI SHALL treat an unmodified left-button press and release in the same cell over the resource list, or over the tree of the split bundle browser, as a navigation click. A navigation click on an unselected resource row SHALL select it; on the already-selected resource it SHALL open that resource's details as Enter does; on a section header it SHALL select that header and fold or unfold it. A navigation click on a bundle-tree file SHALL select it and request its preview unless that file is already previewed, and on a directory SHALL select it and expand or collapse it. A press that moves to another cell, or is followed by a key or wheel event, before release, a click with a modifier or another button, and a click over the header, search row, footer, an empty list row, the right-hand pane, an overlay, a dialog, or the command palette, while a search is being typed, or below the minimum terminal size SHALL NOT perform a TUI action. A navigation click SHALL clear any right-hand text selection and SHALL NOT write to the clipboard. Wheel routing, frame pacing, and right-hand text selection SHALL remain unchanged.

#### Scenario: Clicking a resource row selects it

- **WHEN** the user clicks an unselected resource row, including a row that wraps or follows a scrolled list offset
- **THEN** that row MUST become the selected entry and the right-hand pane MUST show its details
- **AND** the TUI MUST NOT write to the clipboard

#### Scenario: Clicking the selected resource opens its details

- **WHEN** the user clicks the resource row that is already selected
- **THEN** the TUI MUST open that resource's detail view as Enter does

#### Scenario: Clicking a section header toggles its fold

- **WHEN** the user clicks a section header in the resource list
- **THEN** that header MUST become the selected entry and its section MUST fold if expanded or unfold if folded

#### Scenario: Clicking a bundle-tree entry selects and previews it

- **WHEN** the user clicks a file or directory in the tree of the split bundle browser
- **THEN** a file MUST become the selected entry and its preview MUST be requested unless that file is already previewed
- **AND** a directory MUST become the selected entry and expand if collapsed or collapse if expanded

#### Scenario: Clicks that do not target a navigable row are ignored

- **WHEN** a press moves to another cell, or a key or wheel event arrives, before release, a click uses a modifier or a non-left button, or a click lands on the header, search row, footer, an empty list row, the right-hand pane, an overlay, a dialog, or the command palette, or arrives while a search is being typed or below the minimum terminal size
- **THEN** the selection, fold state, and active view MUST remain unchanged and the TUI MUST NOT write to the clipboard

#### Scenario: Terminal clicks navigate the running TUI

- **WHEN** the running TUI receives encoded left press and release input over a resource row, then over the same row again, after a drag that began in the resource list
- **THEN** the drag MUST leave the selection unchanged, the first click MUST show that resource's details, and the second click MUST open its detail view
- **AND** no click MUST emit a clipboard write
