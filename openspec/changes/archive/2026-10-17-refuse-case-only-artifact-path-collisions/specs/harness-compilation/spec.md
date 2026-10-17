## ADDED Requirements

### Requirement: Artifact paths that differ only in letter case collide

Bundle compilation MUST treat two non-fragment artifact paths that differ only in letter case as the same output path, because they name one file on case-insensitive filesystems. Both producing resources MUST be suppressed with the error `bundle.conflict.duplicate-artifact-path` (or `bundle.conflict.cross-kind-artifact-path` for different kinds), naming both paths, and every other resource in the bundle MUST still be emitted. Installation MUST apply the same rule to selected skills, commands, subagents or rules of one kind, for the same target and scope, whose installed names differ only in letter case: each of them MUST be refused with `bundle.conflict.duplicate-artifact-path` before any write, and the rest of the target MUST still install. The rule MUST hold on every host filesystem, so output stays portable between case-sensitive and case-insensitive systems.

#### Scenario: Case-only name collisions are refused portably

- **WHEN** a Codex bundle with agents named `Foo` and `foo` and an agent named `bar` is converted to Codex
- **THEN** neither `Foo` nor `foo` MUST be emitted, compilation MUST report `bundle.conflict.duplicate-artifact-path` naming both paths, and `bar` MUST be emitted

#### Scenario: Case-only name collisions are refused on install

- **WHEN** `sync` installs a directory source whose Codex agents are named `Foo`, `foo` and `bar` to Codex
- **THEN** neither `Foo` nor `foo` MUST be installed, `sync` MUST report `bundle.conflict.duplicate-artifact-path` naming both, and `bar` MUST be installed
