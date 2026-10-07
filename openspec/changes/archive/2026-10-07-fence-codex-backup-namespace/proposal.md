## Why

CreateCodexSet performs path-based directory preparation before capturing its output parent. A late selected-parent alias retarget produces a foreign Codex directory, two valid partition archives and complete manifest, then reports success.

## What Changes

- Apply the existing ordinary archive-output nearest-existing captured parent/EnsurePath pattern in CreateCodexSet before any set member is written.
- Verify the selected named parent generation before descriptor-relative preparation and retain the prepared descriptor through existing set publication/cleanup.
- Preserve cold/warm creation, supported initial external HOME/data/backups/target aliases, archive/manifest modes, empty two-partition set behavior and genuine two-partition restoration.

## Capabilities

### Modified Capabilities

- `backup-recovery`: Codex set namespace creation requires original admitted parent authority.

## Impact

R2 child of #582, stack #560. Existing OpenDir/EnsurePath/parent identity/set publisher reused; no new helper/type/public API/dependency/budget/source-comment/content-policy/durability change. Restore-root namespace preparation remains separate; sampled parent binding is not an atomic writer CAS, complete namespace snapshot or all-directory rollback. Remaining ownership/report-appendix audit stays open.
