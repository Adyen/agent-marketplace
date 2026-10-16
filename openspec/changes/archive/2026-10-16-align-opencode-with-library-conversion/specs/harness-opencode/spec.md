## MODIFIED Requirements

### Requirement: OpenCode is an opt-in harness target and source layout

Agent Marketplace SHALL accept canonical identifier `opencode` for harness target selection, entry-level narrowing, and `source_harness`, and the harness-compilation library SHALL accept it as both the source and the target of conversion. Configuration validation, initialization choices, TUI selection, status, doctor, and backup SHALL expose OpenCode consistently. Existing default harness target lists and existing selections SHALL remain unchanged.

#### Scenario: OpenCode selection reaches consumer workflows

- **WHEN** a user explicitly selects OpenCode through configuration or the TUI
- **THEN** OpenCode SHALL appear in the resolved harness targets and native destination summaries and selected resources SHALL be eligible for installation and backup
- **AND** library conversion SHALL accept `opencode` as both source and target

#### Scenario: Existing configurations retain their harness targets

- **WHEN** an existing configuration is read, synchronized, initialized with existing defaults, or migrated without selecting OpenCode
- **THEN** OpenCode SHALL NOT be added to its harness targets or materialized destinations
