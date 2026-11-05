## Why

When the errors screen shows several diagnostics that share a stable error key, the recovery guidance is attached by key alone. Each displayed cause takes the category of the first diagnostic that carried that key, so a toggle-reload failure shows materialization guidance under the operation cause, and an editor failure whose cause matches a snapshot warning shows the warning's guidance. Operators are then pointed at the wrong next step.

## What Changes

- Key the errors screen's recovery categories by severity and stable key, and consume them in display order so each cause receives its own category.
- Add a scenario to `tui-error-recovery` stating that shared-key diagnostics keep their original cause and their own category guidance, with ordering, counts, identities, and lifetimes unchanged.

## Capabilities

### Modified Capabilities

- `tui-error-recovery`: adds the shared-key diagnostics scenario to the existing safe recovery requirement.

## Impact

- `internal/tui/error_recovery.go` and `internal/tui/help.go` (rendering only).
- `internal/tui/error_recovery_test.go` gains two regression tests bound to the new scenario.
- No change to snapshot application, operation recording, error identity, write guards, or diagnostic lifetimes.
