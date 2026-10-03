## Why

Native Factory command publication authorizes a marker file, then removes a legacy link or writes through a separately reopened pathname. Replacing a classified native command overwrites handwritten bytes and can also permit later stale pruning.

## What Changes

- Reuse the captured portable file publisher for native command bytes; replace admitted legacy links by rename, never unlink before staging.
- Preserve existing file permissions and native byte-identical no-op behavior.
- Reuse a captured command snapshot for both ownership-marker and equality reads instead of a late os.ReadFile, preserving cleanup errors.
- Bind staged publication and cleanup to the attempt-created inode, sharing the guard with regular portable files.

## Capabilities

### Modified Capabilities

- `materialization`: native command publication authority.

## Impact

R2 dependent child of #491. Native regular-file pruning, directory discovery, aggregates, backup/restore, durable ownership and final closure/comment gates remain separate. No new fsync policy, arbitrary size limit or transaction claim.
