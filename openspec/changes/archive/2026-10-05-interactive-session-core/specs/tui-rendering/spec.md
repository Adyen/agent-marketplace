## MODIFIED Requirements

### Requirement: Bounded and safe rendering

The TUI SHALL fit its active view and modal within 50 columns by 12 rows. Sanitization of remote text, paths, diagnostics, errors, configuration values, and Markdown-derived text SHALL follow `session-rendering-safety`. Below the minimum size the TUI SHALL show only a resize-required view, suspend normal input, and allow q and Ctrl+C.

#### Scenario: Hostile input at minimum size

- **WHEN** an untrusted row name or diagnostic contains terminal control bytes and the terminal is 50×12
- **THEN** the control bytes render as visible escapes without changing terminal state or hiding required actions

#### Scenario: Undersized terminal

- **WHEN** the terminal shrinks below 50×12 during a modal
- **THEN** the resize-required view replaces the modal without accepting workflow input, and resizing back restores the prior workflow
