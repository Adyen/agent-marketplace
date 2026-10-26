## ADDED Requirements

### Requirement: Malformed resources show their kind

`list` SHALL label a malformed resource with the kind its failed load was for, such as `command` for a command file whose frontmatter does not parse, and SHALL label it `unknown` when the failure was not classified by kind. `list --kind <kind>` SHALL include a malformed resource only under its own kind, and SHALL include an unclassified malformed entry, such as a registry-level audit diagnostic, under every kind, because no kind can rule it out.

#### Scenario: Malformed resources show their kind

- **WHEN** a registry contains a command file whose frontmatter does not parse and `list` runs
- **THEN** the malformed row MUST show kind `command`
- **AND** `list --kind command` MUST include it and `list --kind skill` MUST NOT
