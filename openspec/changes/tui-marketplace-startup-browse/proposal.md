# Proposal

## Why

Users cannot reliably browse uninstalled Marketplace resources without searching for known names. Startup already initiates empty-query discovery, but the discovery client retains only one page, so complete browsing needs an asynchronous catalog load.

## What Changes

- Load all discoverable resources from configured, allowed Marketplace endpoints in the background on TUI startup, without requiring a query.
- Render cached and arriving resources progressively while preserving navigation, search, installed rows, and existing verification guards.
- Retain the complete browse inventory when search is cleared; avoid repeated full loads from snapshot stages or continuous polling.
- Keep endpoint failures visible and isolated while preserving useful cached results and successful endpoints.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-startup`: complete asynchronous Marketplace browse discovery and continuity.

## Impact

TUI startup/discovery lifecycle, internal Marketplace discovery pagination and cache, tests, terminal QA, and TUI reference documentation. No public API, resource installation, configuration format, or periodic polling change.

## Release note

The TUI loads Marketplace resources in the background when it opens, so you can browse the complete available catalog without searching first.
