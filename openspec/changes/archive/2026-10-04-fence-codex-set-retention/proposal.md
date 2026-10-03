## Why

Codex set retention deletes selected archives and manifests through paths after metadata loading has released directory authority. A hardlinked selected member is removed rather than refusing the whole selected removal plan.

## What Changes

- Reuse the captured backup target directory throughout Codex metadata loading and retention.
- Carry opened manifest identity alongside parsed claims without changing serialized metadata.
- Preflight selected manifest/member generations before deletion; revalidate manifest authorization before each archive removal.
- Reuse ordinary descriptor-relative generation-checked removal with constant member descriptor usage.

## Capabilities

### Modified Capabilities

- `backup-recovery`: Codex set retention authority.

## Impact

R2 child of #509. Preserve existing complete-set filtering, day/per-day ordering and manifest-last removal. Unsafe selected member shape refuses before plan mutation. Initial directory preparation, remaining restore generation and ownership consumers remain separate. No new work limits, fsync, batch transaction or writer-exclusion claim.
