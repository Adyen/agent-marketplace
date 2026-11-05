## MODIFIED Requirements

### Requirement: Resources that share a native path compete for it

On a harness target that installs resources of different kinds at the same native path, such as a skill and a converted command skill on Devin CLI and Codex, resources with the same installed name in one install scope SHALL compete for that path under the precedence of same-kind name collisions: a required selection outranks a non-required one, then the higher-precedence layer or file wins, then the alphabetically first normalized origin. Across those kinds, installed names that differ only in letter case SHALL compete in the same way on every host, because they name one path on case-insensitive filesystems; resources of one kind whose names differ only in letter case follow the case-only collision rule of the `harness-compilation` capability. Only the winner MUST be installed at that path, the path MUST be written once, and `sync` MUST report a `materialize.collision.shared-path` warning that names the target, the path, the winner and the displaced resource. The displaced resource MUST still be installed on targets where its path does not collide, and `status` MUST NOT list the targets where it was displaced. Resources whose kinds have separate paths on a target MUST NOT compete there.

#### Scenario: Required skill keeps a native path a same-name command shares

- **WHEN** an organization source requires skill `audit` and the user's source selects command `audit`, both targeting Devin CLI, Codex and Claude Code, in either materialization mode and whatever order the two are resolved or written in
- **THEN** `sync` MUST install the required skill at `skills/audit/SKILL.md` for Devin CLI and at `.agents/skills/audit/SKILL.md` for Codex, and report a `materialize.collision.shared-path` warning naming the skill and the command
- **AND** the command MUST still be installed for Claude Code, next to the skill, and `status` MUST list the command for Claude Code only
- **AND** `sync --check` MUST succeed afterwards

#### Scenario: Required skill keeps a native path a case-variant command shares

- **WHEN** an organization source requires skill `audit` and the user's OpenCode-layout source selects command `Audit`, both targeting Devin CLI, Codex and Claude Code, in either materialization mode
- **THEN** `sync` MUST install the required skill at the `audit` path for Devin CLI and Codex, MUST NOT install an `Audit` entry beside it on any host, and MUST report a `materialize.collision.shared-path` warning naming the command
- **AND** the command MUST still be installed for Claude Code
- **AND** `sync --check` MUST succeed afterwards
