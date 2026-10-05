## Why

Skill-cache support pruning walks and deletes through public pathnames. Retargeting the admitted cache can delete foreign support files and publish a link to the foreign cache while reporting a successful native sync.

## What Changes

- Capture the cache directory before support-prune discovery.
- Reuse scoped directory reads and descriptor-relative recursive removal.
- Verify the named cache directory still denotes the captured object before discovery and each removal.
- Keep compiled-artifact preservation and fully managed support reconciliation policy.

## Capabilities

### Modified Capabilities

- `materialization`: support-cache prune namespace authority.

## Impact

R2 child of #531 in stack #478. No new public API, dependencies, size budgets or source-comment changes. Initial selected root aliases remain compatible. This does not make compiled-cache publication, support copying and native link publication transactional; support publication/staging cleanup remain separate follow-ons.
