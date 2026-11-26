# Proposal

## Why

Place recovery can observe that a marked file mutation already happened, then advance the journal without confirming the destination parent is durable. A parent-sync failure must keep recovery pending so a retry can establish durability before progress advances.

## What Changes

- Require recovery to sync the verified effective destination parent before completing marked file or ownership publications and marked regular-file prunes whose visible result is already present.
- Keep the operation pending on sync failure and allow a successful retry to finish without repeating the mutation; preserve no-op and read-only behavior.

## Capabilities

### New Capabilities

### Modified Capabilities

- `target-profile-place`: define parent-sync durability requirements for marked file mutation recovery.

## Impact

The Place recovery durability contract and its focused recovery tests change. No public command, API, or dependency changes.

## Release note

Recovery now keeps marked Place file operations pending if syncing their destination parent fails, then completes them on a successful retry without repeating the mutation.
