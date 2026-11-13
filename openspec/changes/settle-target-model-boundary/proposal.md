# Proposal

## Why

The target-profile design assigns application delivery policy to translation metadata and leaves accepted kinds implicit. Settle those boundaries before the accepted-kind refactor starts.

## What Changes

- Update the target-profile design with one application target authority and its translation-library boundary.
- Record the existing compiled-target kind and delivery matrix, glossary decisions, and a bounded implementation brief.
- Leave production behavior and normative specifications unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This design-only change sets `skip_specs: true`.

## Impact

Only `docs/plans/2026-11-13-target-profiles-design.md` and this planning record change. No production implementation, consumer format, lock format, CLI, TUI, or materialized output changes.

## Release note

none - Internal design clarification only; supported behavior does not change.
