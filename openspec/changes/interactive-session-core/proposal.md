## Why

A second interactive client needs the same staged session, navigation, feedback, and guarded-action contract already specified for the TUI. Extracting the surface-neutral behavior now lets the TUI move onto a shared core without changing its user-visible behavior.

## What Changes

- Move surface-neutral interactive behavior into five `session-*` capabilities and retain terminal-specific behavior in the existing `tui-*` capabilities.
- Move surface-neutral startup requirements to `session-startup` while retaining TUI-only provisional-row, footer, cursor/line anchoring, and keyboard-mutation scenarios in `tui-startup`.
- Split the affected TUI requirements where they combine shared behavior with terminal mechanics, and add a neutral rendering-safety requirement.
- Extract an interactive-session core for staged snapshots, progress, guarded actions, lock/lifecycle setup, and first-configuration detection; migrate the TUI to an adapter without behavior change.
- Preserve the established TUI journeys and move or add scenario annotations with the requirements they prove.

## Capabilities

### New Capabilities
- `session-startup`: Surface-neutral staged startup snapshots, provisional rows, generation isolation, and ordinary reload behavior.
- `session-navigation`: Surface-neutral browsing, search, highlighting, inspection, and status-grouped resource-list behavior.
- `session-feedback`: Surface-neutral progress, operation status, and bounded notifications.
- `session-interactions`: Surface-neutral harness selection, guarded mutations, and the legacy user configuration migration offer.
- `session-rendering-safety`: Inert rendering of hostile text for interactive clients.

### Modified Capabilities
- `tui-startup`: Split user-witnessable startup requirements into `session-startup` and retain TUI-only scope, modal, footer, cursor/line, and key-binding scenarios.
- `tui-navigation`: Move complete surface-neutral requirements and split shared search/list/filter behavior from terminal input and layout mechanics.
- `tui-feedback`: Move observed progress, stale progress isolation, and bounded notifications.
- `tui-interactions`: Split neutral selection and guarded mutation behavior from key bindings and modal mechanics.
- `tui-rendering`: Retain terminal sizing and resize behavior while referencing shared sanitization behavior.

## Impact

The implementation layer covers the interactive TUI model and lifecycle, command startup wiring, and their tests. Existing TUI tests and TUI QA journeys remain behavior witnesses; annotations for moved scenarios change to the new `session-*` paths. No user-visible behavior changes in this layer.
