# Proposal

## Why

The configured SkillSpector worker has one scan slot, but overlapping requests from one client can currently reach it concurrently and receive an immediate unavailable response. Queueing those requests in the client preserves the worker's single-slot contract without changing scan budgets.

## What Changes

- Limit each scanner's per-host HTTP connections so overlapping scans wait for the worker slot instead of triggering a concurrent runner request.
- Preserve existing deadlines and report semantics; do not add retries, increase timeouts, add a scheduler, or change schemas.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-analysis`: overlapping scans to a configured single-worker analyzer wait within their existing scan deadlines.

## Impact

Changes `pkg/marketplace/advisory` scanner transport behavior and adds regression coverage. No API, dependency, or report-schema changes.

## Release note

Overlapping SkillSpector requests through the same scanner instance queue behind the active request and retain their existing deadlines; a queued scan can still time out before reaching the worker.
