## MODIFIED Requirements

### Requirement: Theme and composed views

The TUI SHALL use semantic status, accent, selection, muted and border roles mapped to the terminal's ANSI palette, retain distinguishable no-color status labels, and compose list, modal and notification views without hiding required keys or displacing selection. The TUI SHALL use the cell-motion mouse-reporting behavior defined by `tui-mouse-navigation` without enabling all-motion tracking.

#### Scenario: Colorless and narrow rendering

- **WHEN** the display has no color or is at the minimum supported dimensions
- **THEN** status remains readable and modal and quit controls remain within physical bounds

#### Scenario: Mouse reporting agrees with wheel navigation

- **WHEN** the normal TUI program enters the alternate screen
- **THEN** it MUST enable cell-motion mouse reporting for wheel navigation without enabling all-motion tracking

## ADDED Requirements

### Requirement: Frame-paced list integration preserves TUI lifecycle outcomes

The normal TUI program SHALL use resource-list wheel presentation pacing without changing other panes' wheel behavior, editor handoff, interruption handling, or terminal restoration. A failed sync-and-quit operation SHALL still return its sanitized error with the original error identity after terminal restoration.

#### Scenario: Pacing retains terminal and error ownership

- **WHEN** the normal TUI program exits after paced resource-list input, including a sync-and-quit failure, an interrupt, or an editor handoff followed by exit
- **THEN** it MUST retain the existing terminal-restoration and interruption behavior
- **AND** a sync-and-quit failure MUST remain detectable through its original error identity without exposing raw terminal controls
- **AND** wheel input in other panes MUST keep its existing immediate rendering and routing
