# exact-snapshot-verification Specification

## Purpose
Defines how long a verified exact snapshot stays trusted within a command or TUI
action, and which repository metadata an exact snapshot or its object store may
carry.

## Requirements

### Requirement: Exact snapshot verification lasts one command or TUI action

Every CLI command and every TUI action (inspection reload, sync, or update)
SHALL verify an exact snapshot against its algorithm-qualified commit, tracked
tree, and repository metadata before it first reads that snapshot. A
verification SHALL NOT be relied on by any later command or TUI action. A
failed verification SHALL NOT be reused: the next open SHALL verify again. When
a command republishes an exact snapshot, it SHALL verify the republished
snapshot before reading it.

#### Scenario: Snapshot tampered between commands is rejected by the next read-only command

- **GIVEN** a repository whose `sync --locked` has just succeeded from an exact snapshot
- **WHEN** a tracked file inside that exact snapshot is modified and `sync --check` is run
- **THEN** `sync --check` MUST exit nonzero with a `lock.snapshot-unavailable` diagnostic
- **AND** configuration, acquisition locks, ownership state, and materialized output MUST remain byte-identical

#### Scenario: Snapshot tampered between commands is repaired by the next sync

- **GIVEN** a repository whose `sync --locked` has just succeeded from an exact snapshot
- **WHEN** a tracked file inside that exact snapshot is modified and `sync --locked` is run again
- **THEN** the command MUST succeed from a re-acquired exact snapshot whose tracked tree matches the locked commit
- **AND** the acquisition lock MUST remain byte-identical

#### Scenario: Snapshot tampered between TUI actions is reported on reload

- **GIVEN** the TUI has inspected a repository whose exact snapshot verified successfully
- **WHEN** a tracked file inside that exact snapshot is modified and the TUI reloads
- **THEN** the reload MUST report the exact snapshot as an error rather than as in sync

### Requirement: Exact snapshot repository metadata acceptance is independent of formatting

Whether an exact snapshot or its Git object store is accepted SHALL depend only
on its effective repository-local configuration keys, object format, `HEAD`,
worktree root, and file modes. The byte formatting of its repository metadata
SHALL NOT change the result. Repository-local configuration that contains a key
outside the exact-snapshot allowlist SHALL be rejected however it is spelled,
cased, quoted, commented, or included. Repository metadata that Git accepts and
that contains only allowlisted keys SHALL be accepted.

#### Scenario: Equivalent non-canonical repository metadata is accepted

- **GIVEN** an exact snapshot whose repository-local configuration contains only allowlisted keys but uses comments, alternate spacing, or alternate key casing
- **WHEN** `sync --check` reads the exact snapshot
- **THEN** the command MUST succeed as it would for the canonical form of the same configuration

#### Scenario: Disallowed repository configuration is rejected however it is written

- **GIVEN** an exact snapshot whose repository-local configuration adds a key outside the allowlist, such as `core.worktree` or `include.path`, written with an uppercase section name, a quoted value, a trailing comment, or a continuation line
- **WHEN** `sync --check` reads the exact snapshot
- **THEN** `sync --check` MUST exit nonzero with a `lock.snapshot-unavailable` diagnostic
- **AND** configuration, acquisition locks, ownership state, and materialized output MUST remain byte-identical
