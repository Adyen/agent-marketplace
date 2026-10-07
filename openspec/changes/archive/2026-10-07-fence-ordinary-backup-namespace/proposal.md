## Why

Ordinary/mapped Create prepares backup directories with path-based MkdirAll before archive-output authority is captured. Retargeting the selected parent makes it create a foreign target directory and publish a valid archive there, reporting success.

## What Changes

- Reuse existing withArchiveOutput for lazy nearest-existing-parent capture, named identity verification and descriptor-relative missing-directory preparation.
- Keep directory preparation and private archive publication under the same captured parent rather than precreate by path and recapture.
- Preserve cold/warm creation, supported initial aliases through HOME/data/backups/target, archived modes, missing mapped source root and empty-source no-op; leave target-alias listing skip policy unchanged.

## Capabilities

### Modified Capabilities

- `backup-recovery`: ordinary backup namespace creation retains original admitted parent generation.

## Impact

R2 child of #578, stack #560. Existing OpenDir/EnsurePath/archive-parent verification/private archive writer reused; no new helper/type/public API/dependency/budget/source-comment/content/durability change. Codex set creation and restore root namespace preparation remain separate; sampled authority is not atomic uncooperative-writer exclusion, complete namespace snapshot or all-directory rollback.
