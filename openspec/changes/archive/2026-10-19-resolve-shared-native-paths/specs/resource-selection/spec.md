## ADDED Requirements

### Requirement: Resources that share a native path compete for it

On a harness target that installs resources of different kinds at the same native path, such as a skill and a converted command skill on Devin CLI and Codex, resources with the same installed name in one install scope SHALL compete for that path under the precedence of same-kind name collisions: a required selection outranks a non-required one, then the higher-precedence layer or file wins, then the alphabetically first normalized origin. Only the winner MUST be installed at that path, the path MUST be written once, and `sync` MUST report a `materialize.collision.shared-path` warning that names the target, the path, the winner and the displaced resource. The displaced resource MUST still be installed on targets where its path does not collide. Resources whose kinds have separate paths on a target MUST NOT compete there.

#### Scenario: Required skill keeps a native path a same-name command shares

- **WHEN** an organization source requires skill `audit` and the user's source selects command `audit`, both targeting Devin CLI, Codex and Claude Code, in either materialization mode and whatever order the two are resolved or written in
- **THEN** `sync` MUST install the required skill at `skills/audit/SKILL.md` for Devin CLI and Codex and report a `materialize.collision.shared-path` warning naming the skill and the command
- **AND** the command MUST still be installed for Claude Code, next to the skill
- **AND** `sync --check` MUST succeed afterwards
