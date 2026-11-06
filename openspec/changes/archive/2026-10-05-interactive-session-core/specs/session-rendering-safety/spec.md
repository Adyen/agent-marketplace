## Purpose

An interactive client is the TUI or the desktop GUI acting on the user's behalf. This capability defines how hostile text from untrusted sources remains inert and readable in either client.

## ADDED Requirements

### Requirement: Hostile text is inert

An interactive client SHALL render remote text, paths, diagnostics, errors, configuration values, and Markdown-derived text so they cannot inject control sequences or active content into the client, while printable Unicode remains readable.

#### Scenario: Hostile text is inert

- **WHEN** remote text, a path, diagnostic, error, configuration value, or Markdown-derived text contains control sequences or active content
- **THEN** the interactive client SHALL render it without changing client state or activating content
- **AND** printable Unicode SHALL remain readable
