## ADDED Requirements

### Requirement: Malformed rows show their kind

A malformed TUI row SHALL show the kind its failed load was for in its label and detail, and SHALL show `unknown` when the failure was not classified by kind.

#### Scenario: Malformed rows show their kind

- **WHEN** a registry reports a malformed command and an unclassified malformed entry
- **THEN** the command's row MUST be labelled `command <registry>/<name>`, and its detail MUST show `Kind: command`
- **AND** the unclassified entry MUST be labelled with kind `unknown`
