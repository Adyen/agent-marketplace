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
