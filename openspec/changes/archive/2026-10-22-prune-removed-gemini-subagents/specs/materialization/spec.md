## ADDED Requirements

### Requirement: Deselected Gemini subagents are removed

Agent Marketplace SHALL treat the `agents/` directory of a Gemini target as managed output in repository and machine scope, like its `skills/` and `commands/` directories. When a subagent is no longer selected for Gemini, including when it was the last selected Gemini subagent or the last selected resource of the scope, `sync` SHALL remove its output in symlink and file materialization mode, and `deinit` SHALL remove it with the rest of the scope's managed output. Handwritten files in that directory MUST be preserved. `sync --check` MUST report no drift once the output is removed, and machine-scope safety backups SHALL archive the directory.

#### Scenario: Deselected Gemini subagents are removed

- **WHEN** a synced Gemini subagent is no longer selected, with or without other selected Gemini resources, next to a handwritten Gemini subagent
- **THEN** `sync` MUST remove the subagent's output in repository and machine scope and in both materialization modes
- **AND** the handwritten subagent and the remaining selected resources MUST stay installed
- **AND** `sync --check` MUST report no drift

#### Scenario: Deinit removes Gemini subagents

- **WHEN** `deinit` runs for a scope holding a synced Gemini subagent next to a handwritten one
- **THEN** it MUST remove the synced subagent and preserve the handwritten one

#### Scenario: Machine-scope backups archive Gemini subagents

- **WHEN** a Gemini subagent is synced in machine scope
- **THEN** the managed paths of a machine-scope backup MUST include the Gemini `agents` directory

#### Scenario: A symlinked Gemini agents directory is refused without a selected subagent

- **WHEN** `.gemini/agents` is a symbolic link to an existing directory and no Gemini subagent is selected, with or without other selected Gemini resources
- **THEN** `sync` and `sync --check` MUST report `materialize.destination.error` naming the link's absolute path, as for an unsafe output-directory ancestor
- **AND** the link and the directory it points to MUST stay unchanged

#### Scenario: A dangling Gemini agents link is ignored without a selected subagent

- **WHEN** `.gemini/agents` is a symbolic link to a path that does not exist and no Gemini subagent is selected, with or without other selected Gemini resources
- **THEN** `sync` MUST succeed without a diagnostic and install the other selected Gemini resources
- **AND** the link MUST stay unchanged
