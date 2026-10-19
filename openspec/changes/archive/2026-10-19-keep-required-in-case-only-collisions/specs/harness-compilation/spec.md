## MODIFIED Requirements

### Requirement: Artifact paths that differ only in letter case collide

Bundle compilation MUST treat two non-fragment artifact paths that differ only in letter case, compared by Unicode simple case folding, as the same output path, because they name one file on case-insensitive filesystems. Both producing resources MUST be suppressed with the error `bundle.conflict.duplicate-artifact-path` (or `bundle.conflict.cross-kind-artifact-path` for different kinds), naming both paths, and every other resource in the bundle MUST still be emitted. Installation MUST apply the same rule to selected skills, commands, subagents or rules of one kind, for the same target and scope, whose installed names differ only in letter case: each of them MUST be refused with `bundle.conflict.duplicate-artifact-path` before any write, and the rest of the target MUST still install. The exception is a member selected by a required entry that outranks every other member, either because the others are not required or because it comes from a higher-precedence layer or file than each other required member. That member MUST be installed, and each other member MUST be omitted on that target with the warning `materialize.collision.shared-path`, which names both paths. The rule MUST hold on every host filesystem, so output stays portable between case-sensitive and case-insensitive systems.

#### Scenario: Case-only name collisions are refused portably

- **WHEN** a Codex bundle with agents named `Foo` and `foo` and an agent named `bar` is converted to Codex
- **THEN** neither `Foo` nor `foo` MUST be emitted, compilation MUST report `bundle.conflict.duplicate-artifact-path` naming both paths, and `bar` MUST be emitted

#### Scenario: Case-only name collisions are refused on install

- **WHEN** `sync` installs a directory source whose Codex agents are named `Foo`, `foo` and `bar` to Codex
- **THEN** neither `Foo` nor `foo` MUST be installed, `sync` MUST report `bundle.conflict.duplicate-artifact-path` naming both, and `bar` MUST be installed

#### Scenario: Unicode case-folded paths collide

- **WHEN** two resources produce artifact paths that differ only by the Greek sigma `σ` and final sigma `ς`
- **THEN** neither artifact MUST be emitted, and compilation MUST report `bundle.conflict.duplicate-artifact-path` naming both paths

#### Scenario: Required resource keeps a case-only collision

- **WHEN** an organization source requires command `deploy`, and the user's OpenCode-layout source selects command `Deploy`, both for Claude Code
- **THEN** `sync` MUST succeed and install the required command as `commands/deploy.md` with its content
- **AND** `sync` MUST NOT install `Deploy` and MUST report `materialize.collision.shared-path` naming it, without `bundle.conflict.duplicate-artifact-path`
- **AND** `sync --check` MUST succeed afterwards
