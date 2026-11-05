# Proposal

## Why

Review of PR #1002 found that a refresh request can coalesce onto an active refresh job whose worker died while holding its final permitted attempt. Such a job stays `leased` with an expired lease. No path can lease it again, and the crashed worker cannot record an outcome, so refresh requests for the same desired commit and generation stay suppressed until the desired state changes.

## What Changes

- A leased refresh job counts as an active refresh only while it has attempts remaining or its lease is still valid.
- A refresh request whose only matching job is a leased job on its final attempt with an expired lease creates a replacement refresh job.
- The migration tests no longer hardcode the applied migration count, so appending a migration does not break them.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-registry-observation`: refresh coalescing defines which refresh jobs are active.

## Impact

- Registry observation coalescing in the SQLite and PostgreSQL metadata store backends and their shared helper.
- No API, CLI, TUI, web, configuration, or operator documentation change.

## Release note

none — the registry observation store is not yet exposed to users; this corrects unreleased behavior.
