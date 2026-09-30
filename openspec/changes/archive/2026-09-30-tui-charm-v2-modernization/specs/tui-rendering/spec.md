## Purpose

Defines accessible terminal rendering and lifecycle guarantees for a keyboard-only interface using the user's terminal color palette.

## ADDED Requirements

### Requirement: Bounded and safe rendering
The TUI SHALL fit its active view and modal within 50 columns by 12 rows and sanitize remote text, paths, diagnostics, errors, configuration values and Markdown-derived text before terminal output. Printable Unicode SHALL remain readable. Below the minimum size the TUI SHALL show only a resize-required view, suspend normal input, and allow q and Ctrl+C.

#### Scenario: Hostile input at minimum size
- **WHEN** an untrusted row name or diagnostic contains terminal control bytes and the terminal is 50×12
- **THEN** the control bytes render as visible escapes without changing terminal state or hiding required actions

#### Scenario: Undersized terminal
- **WHEN** the terminal shrinks below 50×12 during a modal
- **THEN** the resize-required view replaces the modal without accepting workflow input, and resizing back restores the prior workflow

### Requirement: Theme and composed views
The TUI SHALL use semantic status, accent, selection, muted and border roles mapped to the terminal's ANSI palette, retain distinguishable no-color status labels, and compose list, modal and notification views without hiding required keys or displacing selection. The TUI SHALL NOT enable mouse tracking.

#### Scenario: Colorless and narrow rendering
- **WHEN** the display has no color or is at the minimum supported dimensions
- **THEN** status remains readable and modal and quit controls remain within physical bounds

### Requirement: Terminal restoration
The TUI SHALL restore captured termios, disable bracketed paste, show the cursor, and leave the alternate screen on clean quit, handled signal, startup failure or returned error. Only the interrupt sentinel SHALL be treated as a clean interrupt; other errors SHALL preserve their identity and be escaped before printing.

#### Scenario: Error after entering alternate screen
- **WHEN** the interactive program returns an error while the alternate screen is active
- **THEN** terminal settings are restored and the original error remains detectable
