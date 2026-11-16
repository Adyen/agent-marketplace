# Proposal

## Why

The target-profile design leaves place-delivery staging, publication, crash recovery, root-bound ownership, whole-set conflict planning, edited owned files and failed verification unresolved. Those choices decide what a running host can observe, what survives an interruption and which location a sync may write or clean up. Settle them against the existing filesystem, materialization, dependency-transaction, ownership and package-generation seams before the place-delivery contract is frozen.

## What Changes

- Update the target-profile design with the selected staging and publication units, how engine staging outside target roots coexists with contained publication and no dot-prefixed staging siblings, cross-filesystem behavior, host hot-reload observation limits and package/dependency partial visibility.
- Add a state-transition table for interruption before, during and after publication, durable ownership commit and pruning, naming the existing journal and generation seams reused and the exact read-only and mutating recovery outcomes.
- Bind ownership, status and retirement to canonical recorded installation roots, including environment/root changes and destination symlink replacement.
- Specify whole-set conflict planning, including installed projections, new conflicting packages, unowned byte-identical collisions, prefix/file-directory and case collisions.
- Choose edited-owned-file behavior for sync, upgrade and deselection and failed-verification retention and recovery, each with a future witness, distinguishing initial-load check failure, crash recovery and user edits.
- Leave production behavior, journals, ownership state and normative specifications unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This design-only change sets `skip_specs: true`.

## Impact

Only `docs/plans/2026-11-13-target-profiles-design.md` and this planning record change. No production implementation, filesystem write path, journal, ownership store, register command, CLI, TUI or materialized output changes.

## Release note

none - Internal design proposal only; supported behavior does not change.
