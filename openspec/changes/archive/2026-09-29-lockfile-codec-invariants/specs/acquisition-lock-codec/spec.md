## Purpose

Defines the canonical on-disk form of a written acquisition lock: the exact
YAML bytes Agent Marketplace emits and reads back for layer-owned lockfiles.

## ADDED Requirements

### Requirement: Acquisition lock encoding is deterministic and canonical

The acquisition lock codec SHALL produce a deterministic YAML byte stream;
equal documents SHALL produce identical bytes. Current writers emit the v2
format; the committed v1 golden bytes pin the v1 compatibility schema that
the codec still accepts, not a current CLI write policy.

#### Scenario: Minimal v1 lock emits stable compatibility bytes

- **WHEN** an empty v1 acquisition lock document is encoded for any scope
- **THEN** the emitted bytes SHALL equal the committed v1 golden header for
  that scope

#### Scenario: v1 Git lock emits stable compatibility bytes for every scope

- **WHEN** a populated Git-only v1 acquisition lock document is encoded for
  any scope
- **THEN** the emitted bytes SHALL equal the committed v1 golden bytes

#### Scenario: v2 Marketplace lock emits stable golden bytes

- **WHEN** a populated v2 Marketplace acquisition lock document is encoded
- **THEN** the emitted bytes SHALL equal the committed golden bytes

### Requirement: Canonical form is ordering- and duplicate-insensitive

The order of input collections SHALL NOT change emitted bytes; records SHALL
be emitted in canonical order, and an exact duplicate record SHALL collapse
to a single record.

#### Scenario: Registry input order does not change emitted bytes

- **WHEN** the same registries are written in a different input order
- **THEN** the emitted bytes SHALL be identical

#### Scenario: Marketplace records are ordered and exact duplicates collapse

- **WHEN** Marketplace endpoint and resource records are written unordered
  with an exact duplicate
- **THEN** the emitted bytes SHALL contain each record once in canonical
  order

#### Scenario: Selection edges are ordered and deduplicated

- **WHEN** a resource pin carries repeated or unordered installation
  selectors
- **THEN** the emitted bytes SHALL contain each selector once in canonical
  order

### Requirement: Reading existing lock YAML normalizes to the canonical document

Reading existing acquisition lock YAML SHALL yield the normalized document;
non-canonical spellings of the same identity SHALL canonicalize on read.

#### Scenario: Non-canonical Marketplace endpoint spellings normalize on read

- **WHEN** a lock file contains a Marketplace endpoint URL that differs from
  its canonical form only in casing or a trailing slash
- **THEN** reading SHALL yield the canonical endpoint form

#### Scenario: Emitted bytes decode to the normalized document

- **WHEN** emitted acquisition lock bytes are read back
- **THEN** the decoded document SHALL equal the normalized input document
