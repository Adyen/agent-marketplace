## Why

Legacy registry publication writes current assessments before a same-commit head update can bypass its stale expected-head check. An outdated worker must not replace a newer reject with an older assessment.

## What Changes

- Check the expected effective registry head under a transaction-held registry fence before legacy metadata publication.
- Share the check between supported metadata stores and reject stale empty-head expectations after initialization.
- Keep modern publication-identity retries unchanged; permit legacy same-commit retries with a valid expected head or verified already-current metadata.

## Capabilities

### New Capabilities

- `marketplace-publication`: expected-head validation protects legacy assessment publication.

### Modified Capabilities

## Impact

The metadata-store interface gains a transactional head assertion implemented by SQLite and PostgreSQL. HTTP and stored schemas are unchanged. A legacy caller retrying an initialized head with an empty expectation may succeed only when the requested metadata is already current; stale changed metadata is refused.
