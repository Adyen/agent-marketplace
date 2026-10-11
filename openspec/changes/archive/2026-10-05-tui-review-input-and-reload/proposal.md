## Why

Several accepted PR456 findings identify observable TUI input and resize defects, plus an unnecessary provisional-work path on ordinary reloads. Correcting these now keeps typed input predictable, fixed-choice dialogs responsive, and the unified v4 TUI's final snapshot authoritative without expanding the release direction.

## What Changes

- Preserve `j` and `k` as command-palette query text; use arrow keys for match navigation.
- Keep toggle and config-editor choices non-filterable; restore all harness options after compact-to-full resize in both target-selection dialogs.
- Limit provisional/config/inventory stage presentation and its supporting work to initial startup; ordinary reloads retain the current view until one final authoritative snapshot.
- Strengthen the accepted Sync, rendering, progress, search, theme, and wheel regression witnesses at their real boundaries.
- Correct the archived design's `at50/80columns` spacing typo.

## Capabilities

### New Capabilities

### Modified Capabilities

- `tui-navigation`: Command-palette query characters and arrow-key navigation remain distinct.
- `tui-interactions`: Fixed-choice modal input and harness option visibility remain stable through resize.
- `tui-startup`: Progressive staged presentation is startup-only; ordinary reloads publish one final authoritative snapshot.

## Impact

Changes are limited to the interactive TUI's model/forms/snapshot handling and tests, the TUI QA wheel journey, and the corresponding OpenSpec requirements. No CLI/API, dependency, schema version, release direction, or production wheel instrumentation changes are intended.
