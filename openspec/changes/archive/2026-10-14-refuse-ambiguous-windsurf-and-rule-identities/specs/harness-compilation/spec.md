## ADDED Requirements

### Requirement: Ambiguous Windsurf identities and rule metadata are refused

Harness Compilation MUST NOT choose a winner between two resources or two metadata values that claim the same identity. When two Windsurf workflows or two Windsurf rules in the same workspace root resolve to the same name, ingestion MUST ingest none of them and MUST report the error `ingest.windsurf.duplicate-identity`; other resources MUST still be ingested. A rule whose frontmatter declares the same key twice MUST NOT be ingested.

#### Scenario: Windsurf workflows with the same name are refused

- **WHEN** two Windsurf workflow files in one workspace declare the same name next to an unrelated workflow
- **THEN** conversion MUST report the error `ingest.windsurf.duplicate-identity`, MUST emit no command for that name, and MUST still emit the unrelated workflow

#### Scenario: Rule frontmatter with a repeated key is refused

- **WHEN** a Cursor rule's frontmatter declares `alwaysApply` twice
- **THEN** conversion MUST emit no rule artifact for it and MUST report an error
