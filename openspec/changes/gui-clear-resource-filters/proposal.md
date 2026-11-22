## Why

Removing several resource filters took one click per chip. A user who had a source, a kind and a status filter active had to find and press three separate clear controls to return to the full list.

## What Changes

- The active resource filter bar offers a Clear filters control while any source, kind, or status filter is active. It removes every active filter at once and returns to the unfiltered resource list.
- Each filter's own clear control is unchanged.
- Entering Diagnostics and choosing All resources keep their existing filter clearing, now through the same shared behavior.

## Capabilities

### Modified Capabilities

- `gui-shell`: the filter bar offers a one-step clear of all resource filters.

## Impact

Desktop GUI filter bar and its tests. No configuration, local API, Marketplace, TUI, or resource behavior changes.

## Release note

The desktop app's filter bar has a Clear filters button that removes every active filter at once.
