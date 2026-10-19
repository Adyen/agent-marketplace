## Why

P11 in the #215 frozen ledger, findings A51 and F33 of both reports (#759). Devin CLI and Codex install commands as skills, so a skill and a command with the same installed name write to the same `skills/<name>/SKILL.md`. Collision resolution keys resources by kind and name, so the two never compete: sync writes both, the last write wins, and it reports success with no warning.

When an organization requires a skill and the user selects a command of the same name from a registry or Marketplace, the command can silently replace the required skill. Reproduced on `main` at `f44101fa` over two runs: in every file-mode run, where `sync --check` also reports drift right after `sync`, and in 3 of 4 symlink-mode runs. Write order follows snapshot paths that hash each source and commit, so the symlink outcome varies between runs.

## What Changes

- On every harness target, resources compete for the native path they install to, not only for their kind and name. On Devin CLI and Codex, a skill and a command with the same installed name in one install scope therefore compete under the existing precedence: a required selection wins, then the higher-precedence layer or file, then the alphabetically first normalized origin.
- Only the winner is installed at that path, and the path is written once.
- Sync reports a `materialize.collision.shared-path` warning that names the target, the path, the winner and the displaced resource.
- The displaced resource is still installed on targets where its path does not collide, such as Claude Code.
- Targets whose kinds have separate paths behave as before.

## Capabilities

### Modified Capabilities

- `resource-selection`: resources that share a native path compete for it.

## Impact

`resolver.MaterializationProjection` keys collisions by the target's native resource path pattern, falling back to the kind where a target declares none, and returns displacements across kinds. `MaterializationWinners` keeps its signature for cleanup and removal callers, so they use the same projection. Materialization adds the new warning code to the diagnostic catalog. Configuration, lock and native output formats do not change; users with such a pair get the winner where they previously got whichever was written last.
