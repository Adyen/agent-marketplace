## Why

A review of #774 (CodeRabbit) found a residual case of #759. Resource names may contain upper-case letters (an OpenCode command is named after its file), so an organization-required skill `audit` and a user-selected command `Audit` were separate winners, and the case-only refusal compares names only within one kind. On a case-insensitive filesystem, the default on macOS, both write the same `skills/audit/SKILL.md` on Devin CLI and Codex. Reproduced on a case-insensitive APFS volume at `d7f722af`: the command replaced the required skill in both materialization modes, sync reported success, and `sync --check` reported drift.

The same review found that `status` still listed a displaced resource as installed on the target where it lost the path, and that the scenario named the Devin CLI path for Codex too.

## What Changes

- Across kinds that share a native path, names that differ only in letter case compete for it like equal names, on every host, matching the host-independent case-only rule. The best member under the existing precedence keeps the path, and members of other kinds are displaced with the `materialize.collision.shared-path` warning, which names both paths.
- Resources of one kind whose names differ only in letter case are still refused as case-only collisions.
- `status` omits the targets a resource lost to a shared path, and shows `not materialized: shared path` when it lost all of them.
- The scenario names `.agents/skills/audit/SKILL.md` for Codex.

## Capabilities

### Modified Capabilities

- `resource-selection`: shared native paths compare names without letter case across kinds, and `status` reflects displacement.

## Impact

`resolver.MaterializationProjection` adds a pass that only runs when a name on a shared path contains an upper-case letter. Displacements carry the displaced resource's input index, which `status` uses. On case-sensitive hosts, a skill and a command whose names differ only in letter case no longer both install on Devin CLI or Codex.
