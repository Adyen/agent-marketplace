## MODIFIED Requirements

### Requirement: Git metadata is not a skill candidate
When ingesting a directory source, Agent Marketplace SHALL ignore a `.git` directory beneath a source layout's `skills/` directory as a skill candidate. It SHALL continue to discover authored skills and report malformed authored skills without reporting Git metadata as malformed or as an ignored sub-bundle.

#### Scenario: Git metadata beside valid and malformed skills
- **WHEN** a v4 directory source contains `skills/.git/` with Git metadata, an authored valid skill, and an authored malformed skill
- **THEN** listing the source and opening it in the TUI show the valid skill and the malformed authored skill, and do not report `skills/.git` as a malformed resource or an ignored sub-bundle

#### Scenario: Non-Claude source layouts ignore Git metadata
- **WHEN** a v4 directory source uses a Factory, Cursor, Gemini, Codex, or Windsurf source layout with Git metadata at its skill-directory boundary and an authored valid skill
- **THEN** listing the source discovers the authored skill without listing `.git` as a resource or reporting a Git-metadata error

#### Scenario: Windsurf metadata does not abort discovery
- **WHEN** a Windsurf source layout contains a valid skill beside a `.git` directory in its skills tree
- **THEN** discovery continues to return the valid skill rather than failing the source because `.git` lacks a skill definition

#### Scenario: Factory Git metadata directory symlink is not a skill
- **WHEN** a Factory source layout contains a `.git` directory symlink beside a valid skill in its skills tree
- **THEN** discovery does not follow the Git metadata symlink as a skill candidate and still discovers the valid skill
