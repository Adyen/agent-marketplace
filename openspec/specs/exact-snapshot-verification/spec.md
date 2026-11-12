# exact-snapshot-verification Specification

## Purpose
Defines how long a verified exact snapshot stays trusted within a command or TUI
action, and which repository metadata an exact snapshot or its object store may
carry.

## Requirements

### Requirement: Exact snapshot verification lasts one command or TUI action

Every CLI command and every TUI action (inspection reload, sync, or update)
SHALL verify an exact snapshot against its algorithm-qualified commit, the
tracked tree of the configured root it consumes, and repository metadata before
it first reads that snapshot. A verification SHALL NOT be relied on by any later
command or TUI action. A failed verification SHALL NOT be reused: the next open
SHALL verify again. When a command republishes an exact snapshot, it SHALL
verify the republished snapshot before reading it.

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

### Requirement: Exact snapshots are scoped to the configured root

An exact snapshot acquired for a registry root configured with `path:` SHALL
contain only the commit's tracked files under that root, and its verification
SHALL hash every tracked file under that root and no file outside it. A root
without `path:`, or with `path: .`, SHALL use a snapshot of the whole
repository. Registry entries for the same source and commit with different
paths SHALL each resolve from a snapshot that covers their own root. A
whole-repository exact snapshot published by an earlier release SHALL remain
usable without user action: read-only commands SHALL accept it after verifying
the consumed root inside it, and mutating commands SHALL acquire a
root-scoped snapshot instead. Acquisition locks SHALL be unaffected.

#### Scenario: Snapshot for a configured path contains only that path

- **GIVEN** a registry commit that tracks files under `skills/` and outside it, and a registry entry configured with `path: skills`
- **WHEN** `sync --locked` acquires the exact snapshot
- **THEN** the exact snapshot MUST contain the commit's tracked files under `skills/` and no tracked file outside it
- **AND** `sync --check` MUST succeed from that snapshot

#### Scenario: Tampering inside the configured root is refused

- **GIVEN** a repository whose `sync --locked` has just succeeded from an exact snapshot for a registry entry configured with `path: skills`
- **WHEN** a tracked file under `skills/` in that exact snapshot is modified and `sync --check` is run
- **THEN** `sync --check` MUST exit nonzero with a `lock.snapshot-unavailable` diagnostic
- **AND** configuration, acquisition locks, ownership state, and materialized output MUST remain byte-identical

#### Scenario: Content outside the configured root is not consumed

- **GIVEN** a repository whose `sync --locked` has just succeeded from an exact snapshot for a registry entry configured with `path: skills`
- **WHEN** a file outside `skills/` is created or modified in that exact snapshot and `sync --check` is run
- **THEN** `sync --check` MUST succeed
- **AND** configuration, acquisition locks, ownership state, and materialized output MUST remain byte-identical

#### Scenario: Layers with different paths for one source resolve independently

- **GIVEN** two configuration layers that each configure the same registry source and commit, one with `path: skills` and one with `path: commands`
- **WHEN** `sync --locked` runs and then `sync --check` runs
- **THEN** both commands MUST succeed and materialize the resources of both roots

#### Scenario: Existing whole-repository snapshot remains usable

- **GIVEN** a repository whose acquisition lock pins a registry entry configured with `path: skills`, and whose exact cache holds only a whole-repository snapshot of the locked commit
- **WHEN** `sync --check` is run and then `sync --locked` is run
- **THEN** both commands MUST succeed without user action
- **AND** the acquisition lock MUST remain byte-identical
