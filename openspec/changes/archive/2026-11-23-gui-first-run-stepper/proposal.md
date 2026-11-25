## Why

The first-run step line reads "1 Targets · 2 Telemetry · 3 Sources" as one run of text with middle-dot separators. The steps read as a sentence rather than as a sequence, and the current step is only shown by weight.

## What Changes

- The setup steps are a numbered list of three items: 1 Targets, 2 Telemetry, 3 Sources.
- The separators between steps are removed.
- The current step is marked with `aria-current="step"` and keeps its existing emphasis.

## Capabilities

### Modified Capabilities

- `gui-first-run`: the step indicator presentation.

## Impact

First-run dialog (`gui/src/FirstRun.vue`, `gui/src/style.css`) and its tests. No configuration, local API, or setup behaviour changes.

## Release note

The first-run setup dialog shows its three steps as a numbered list, with the current step marked.
