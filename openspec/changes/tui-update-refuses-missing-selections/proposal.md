## Why

`agent-marketplace update` now refuses before publication when a selection that resolved at the previously locked revision stops resolving at the new one. An update started from the TUI does not apply that check. It still advances the acquisition lock past a renamed or removed selection and drops the selection, which is the behavior the CLI fix removed.

## What Changes

- An update started from the TUI applies the same pre-publication selection check as `update`, refuses with the same text, and leaves the acquisition lock, configuration, and harness outputs unchanged.
- The TUI reports the refusal as the update's failure and reloads from the unchanged lock. It has no new screen or per-registry update action.
- The TUI rule that an optional missing selection does not fail a reload is clarified: an update refusal for a selection that stops resolving at the new revision is a pre-publication refusal, not a reload failure.
- Out of scope: the check's logic and wording, CLI `update`, and TUI sync, install, and toggle.

## Capabilities

### New Capabilities

### Modified Capabilities

- `resource-selection`: the update refusal requirement also covers an update started from the TUI.
- `tui-startup`: the missing-optional-selection rule distinguishes the update refusal from a reload failure.

## Impact

- `cmd/agent-marketplace` TUI dependency lifecycle: the TUI update enables the existing selection guard.
- No lock, configuration, or TUI presentation change.
