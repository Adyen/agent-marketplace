## ADDED Requirements

### Requirement: Malformed-resource details cannot control the terminal

Agent Marketplace MUST print the malformed-resource list of `sync --verbose` without raw terminal control data. Each listed resource name, source path and parse error MUST use the single-line policy that applies to command errors: C0 and C1 control characters, including line breaks inside an entry, bidirectional controls, Unicode line and paragraph separators and invalid UTF-8 MUST appear as visible escapes, and each malformed resource MUST stay on its own line. Printable Unicode MUST be printed unchanged.

#### Scenario: Malformed-resource details cannot control the terminal

- **WHEN** a selected source contains a malformed Devin hook file whose parse error carries a terminal control sequence and a line break, and `sync --verbose` runs
- **THEN** the malformed-resource entry MUST show the control sequence and the line break as visible escapes on a single line, and the output MUST contain no raw escape character
