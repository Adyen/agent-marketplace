# owned-entry-reconciliation Specification

## Purpose
Defines how Agent Marketplace reconciles entries in shared harness aggregate
files against durable ownership state, preserving handwritten entries and
failing closed.

## Requirements

### Requirement: Unproven entries are preserved

An existing entry SHALL be treated as unmanaged unless durable ownership
state proves it is an owned entry; unmanaged entries SHALL be left unchanged.

#### Scenario: Handwritten entries remain unmanaged

- **WHEN** an aggregate entry has no ownership-state record
- **THEN** sync SHALL preserve it unchanged

#### Scenario: Missing ownership state preserves everything

- **WHEN** the ownership state is missing or unreadable
- **THEN** every existing entry SHALL be preserved and none SHALL be pruned

### Requirement: Unmanaged collisions refuse only the generated entry

When a desired generated entry shares the identity of an unmanaged existing
entry, sync SHALL preserve the existing entry and refuse only the generated
entry; valid sibling entries SHALL still materialize.

#### Scenario: Unmanaged collision fails only the generated entry

- **WHEN** a desired MCP key already exists in the file as an unmanaged entry
- **THEN** the existing entry SHALL be preserved and only the generated entry
  SHALL fail

### Requirement: Proven owned entries reconcile against recorded state

A proven owned entry SHALL be kept when its value matches the record,
restored when it has drifted, and pruned only when no longer desired; an
unmanaged entry SHALL never be pruned.

#### Scenario: Drifted owned entry is restored

- **WHEN** a proven owned entry's current value differs from the record
- **THEN** sync SHALL replace it with the configured value

#### Scenario: Stale owned entries are pruned

- **WHEN** a proven owned entry is present but no longer desired
- **THEN** sync SHALL remove it and no unproven entry

### Requirement: Repair candidates partition by value equality

An unmanaged existing entry whose identity is desired SHALL be offered for
adoption when its value equals the desired value, and as a replacement
candidate when it differs; the offers SHALL be disjoint.

#### Scenario: Identical values are offered for adoption

- **WHEN** an unmanaged desired entry's value equals the generated value
- **THEN** repair SHALL offer byte-neutral adoption

#### Scenario: Differing values are offered for replacement

- **WHEN** an unmanaged desired entry's value differs from the generated value
- **THEN** repair SHALL offer it for replacement and never for adoption

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
