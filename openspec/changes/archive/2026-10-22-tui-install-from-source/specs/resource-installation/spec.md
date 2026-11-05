## ADDED Requirements

### Requirement: Explicit-source installs can be bound to a previewed commit

An install from an explicit Git source MAY carry the commit a preview
resolved. When the source's ref no longer resolves to that commit at install
time, the install MUST fail before publishing configuration or acquisition
lock changes or materializing anything, and MUST report both commits. When it
matches, the acquisition lock MUST record that commit while configuration
keeps the requested ref. An expected commit for a directory source MUST be
refused before any write. An explicit-source install MAY select resources by
kind and name; resources of different kinds that share a name MUST then be
selected unambiguously, and collections MUST be selectable this way.

#### Scenario: Moved source refuses an expected-commit install

- **WHEN** an explicit-source install carries an expected commit and the
  source's ref has advanced to a different commit
- **THEN** the install MUST fail naming both commits and MUST leave
  configuration, acquisition locks, and materialized resources unchanged

#### Scenario: Matching expected commit installs at the requested ref

- **WHEN** an explicit-source install carries the commit the source's ref
  currently resolves to
- **THEN** configuration MUST record the requested ref and the acquisition
  lock MUST record that commit

#### Scenario: Expected commit for a directory source is refused

- **WHEN** an explicit-source install from a directory carries an expected
  commit
- **THEN** the install MUST fail before any write

#### Scenario: Kind-qualified picks select resources that share a name

- **WHEN** an explicit-source install selects a skill and a command that share
  a name, and a collection, by kind and name
- **THEN** exactly those resources MUST be selected from that source and
  materialized, without an ambiguity error
