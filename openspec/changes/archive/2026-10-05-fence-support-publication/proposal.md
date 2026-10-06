## Why

Support snapshots use the path-based temporary writer. A parent replaced after staging can make native sync rename a foreign replacement staged file into support output and report success.

## What Changes

- Route the shared support snapshot writer through the existing captured portable byte publisher, retaining its classified destination generation.
- Expose the existing staging boundary with a structured debug log (paths/phase, no payloads).
- Keep no-op byte/mode checks, source mode and source admission unchanged.

## Capabilities

### Modified Capabilities

- `materialization`: captured support-file publication.

## Impact

Child of #533 in R2 stack #478. No new public API, filesystem framework, dependency, budget or source-comment change. Support-directory creation and producer staging-directory cleanup remain separate follow-ons; not a whole cache/native transaction.
