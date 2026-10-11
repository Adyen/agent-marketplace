# command-error-output Specification

## Purpose
Defines how CLI commands print returned errors and doctor error lists so untrusted values in error text cannot change terminal state, while ordinary error text and error identity stay unchanged.

## Requirements

### Requirement: Command errors cannot control the terminal

Agent Marketplace MUST print every error returned by a CLI command, and every error listed by `doctor`, without raw terminal control data. C0 and C1 control characters, bidirectional controls, Unicode line and paragraph separators and invalid UTF-8 inside a line MUST appear as visible escapes. Line breaks that separate errors MUST remain line breaks. Printable Unicode MUST be printed unchanged. Error identity for `errors.Is` and `errors.As`, exit status and machine-readable output MUST remain unchanged.

#### Scenario: Configuration errors cannot control the terminal

- **WHEN** a repository configuration value carrying terminal controls makes `status`, `list` or `sync` fail, or appears in `doctor`'s configuration errors
- **THEN** the printed error MUST show those controls as visible escapes, keep each joined error on its own line, and keep the typed configuration error identity

#### Scenario: Ordinary command errors are printed unchanged

- **WHEN** a command error contains only printable characters, including non-ASCII letters and punctuation
- **THEN** the printed `Error:` line MUST match the error text byte for byte
