## Why

SDK stage creation still used MkdirTemp(outDir) before capturing root, so late root replacement creates a foreign stage directory even when later root validation refuses. Stage parent preparation/writes/chmod also reopened paths.

## What Changes

- Create/capture missing SDK output below a captured existing ancestor using the shared directory creator.
- Admit root before producer staging, revalidate named root and create private random staging descriptor-relatively.
- Open/match original stage directory and create staged parents/files through it; chmod existing-mode stages through the opened file.
- Preserve original source modes/umask, stage-all-artifacts/dry/partial semantics and prior inode-aware cleanup.

## Capabilities

### Modified Capabilities

- `materialization`: SDK stage creation and writing authority.

## Impact

R2 child of #543 in stack #478. Existing fsboundary/std crypto-random APIs; no new helper/type/public API/dependency/budget/source-comment change. Initial supported ancestor aliases remain selected-root behavior; final root alias rejection unchanged. Individual destination/staged-file leases and stale discovery/removal remain separate; no whole-tree transaction or atomic uncooperative-writer exclusion.
