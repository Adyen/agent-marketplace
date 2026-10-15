## ADDED Requirements

### Requirement: Claude Code resources produce no plugin manifest entries

Installing Claude Code skills and subagents SHALL NOT write a
`.claude-plugin/plugin.json` file under the Claude Code target root in
either scope, and compiled Claude Code output SHALL NOT contain a generated
`.claude-plugin/plugin.json`. Entries that earlier versions wrote there with
the `_managed_by: agent-marketplace` marker SHALL be retired by `sync`, and
entries without that marker SHALL be preserved. The Factory Droid target
SHALL NOT claim a `.factory-plugin` directory under its target root.

#### Scenario: Sync writes no Claude plugin manifest

- **WHEN** `sync` installs a skill or a subagent for Claude Code in the
  repository scope or the machine scope
- **THEN** it MUST NOT create or modify `.claude-plugin/plugin.json` under
  the Claude Code target root
- **AND** `convert` output for Claude Code from a layout with skills or
  subagents MUST NOT contain a generated `.claude-plugin/plugin.json`

#### Scenario: Sync retires earlier managed manifest entries

- **WHEN** `.claude/.claude-plugin/plugin.json` or
  `~/.claude/.claude-plugin/plugin.json` contains entries carrying the
  `_managed_by: agent-marketplace` marker and `sync` runs for that scope
- **THEN** `sync` MUST remove those entries
- **AND** entries without the marker and other members MUST remain unchanged
- **AND** when only empty `skills` and `agents` members remain, `sync` MUST
  delete the file and the then-empty `.claude-plugin` directory

#### Scenario: Check reports pending manifest retirement without writing

- **WHEN** managed entries remain in a Claude Code `.claude-plugin/plugin.json`
  and `sync --check` runs
- **THEN** it MUST report drift
- **AND** it MUST NOT modify the file

#### Scenario: Factory plugin directory is left alone

- **WHEN** a Factory Droid target root contains a `.factory-plugin`
  directory and `sync` runs
- **THEN** the directory and its contents MUST remain unchanged
