## Why

The GUI branch lacks the shared Marketplace catalog pager and cache merged in PR #1066. Its existing empty-query discovery stops after one page. Importing the shared implementation lets the desktop branch browse more than one page and lets the later source-browsing change reuse it.

## What Changes

- Use the merged shared Marketplace browse implementation on the GUI branch so the existing empty-query discovery can emit multiple pages and retain partial results honestly.
- Preserve existing search routing, source controls, endpoint policy, and resource mutations.

## Capabilities

### Modified Capabilities

- `gui-shell`: Existing unfiltered Marketplace discovery can show resources beyond the first page and report a partial catalog on failure.

## Impact

Shared Marketplace discovery and its tests, plus desktop regression coverage. No source filter, separate GUI browse inventory, TUI edits, Marketplace server changes, or consumer configuration changes.
