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

### Requirement: Malformed-resource details cannot control the terminal

Agent Marketplace MUST print the malformed-resource list of `sync --verbose` without raw terminal control data. Each listed resource name, source path and parse error MUST use the single-line policy that applies to command errors: C0 and C1 control characters, including line breaks inside an entry, bidirectional controls, Unicode line and paragraph separators and invalid UTF-8 MUST appear as visible escapes, and each malformed resource MUST stay on its own line. Printable Unicode MUST be printed unchanged.

#### Scenario: Malformed-resource details cannot control the terminal

- **WHEN** a selected source contains a malformed Devin hook file whose parse error carries a terminal control sequence and a line break, and `sync --verbose` runs
- **THEN** the malformed-resource entry MUST show the control sequence and the line break as visible escapes on a single line, and the output MUST contain no raw escape character
