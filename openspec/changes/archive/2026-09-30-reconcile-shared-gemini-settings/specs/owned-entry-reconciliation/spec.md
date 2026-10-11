## ADDED Requirements

### Requirement: Gemini shared scopes reconcile together

When the repository root and user home identify the same directory, including filesystem aliases, Agent Marketplace SHALL reconcile the shared Gemini native settings file once against the combined desired entries. It MUST preserve handwritten content and existing permissions, and identical synchronization MUST NOT rewrite the file. An entry SHALL be pruned only when it is no longer desired in either scope. Conflicting values for one MCP identity and operations that cannot isolate the requested scope MUST refuse before materialization.

#### Scenario: Gemini shared settings retain both scopes

- **WHEN** repository and machine selections share one Gemini settings file
- **THEN** MCP servers and hooks from both scopes MUST remain present alongside handwritten entries
- **AND** repeated synchronization and check-only inspection MUST report no drift without rewriting bytes or modification time
- **AND** removing one scope's selections MUST preserve the other scope and handwritten entries

#### Scenario: Gemini shared identity conflict refuses mutation

- **WHEN** two scopes desire different values for the same MCP identity in one Gemini settings file
- **THEN** materialization MUST report `materialize.destination.overlap`
- **AND** existing settings and ownership state MUST remain unchanged

#### Scenario: Gemini shared scope-restricted mutation refuses

- **WHEN** a scope-restricted operation targets a Gemini root shared by repository and machine scopes
- **THEN** it MUST report `materialize.destination.overlap` before writing or pruning
- **AND** the existing settings MUST remain unchanged
