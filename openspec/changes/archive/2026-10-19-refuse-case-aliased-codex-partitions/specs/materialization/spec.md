## ADDED Requirements

### Requirement: Codex partitions that alias one location are refused

Codex destination preflight SHALL treat managed paths of different Codex partitions as overlapping when they are spelled differently but name the same location: paths that differ only in letter case, on every host, or paths below the same existing directory reached through another spelling. Materialization MUST then refuse with `materialize.destination.overlap`, naming both spellings, before any destination mutation and whether or not the selected resources conflict. Partitions whose managed paths remain disjoint MUST stay usable even when their roots differ only in letter case, and partitions with identical spellings MUST keep coalescing.

#### Scenario: Codex partitions that differ only in letter case are refused

- **WHEN** CODEX_HOME spells the repository's `.codex` directory with different letter case and both repository and machine scope select Codex output, whether conflicting or disjoint
- **THEN** materialization MUST refuse with `materialize.destination.overlap` naming both spellings, on case-sensitive and case-insensitive hosts alike
- **AND** neither spelling MUST receive any write

#### Scenario: Disjoint Codex partitions that differ only in letter case remain usable

- **WHEN** CODEX_HOME relocates the Codex configuration partition to a spelling of the skills partition root that differs only in letter case
- **THEN** both the skill and the configuration output MUST install without an error diagnostic
