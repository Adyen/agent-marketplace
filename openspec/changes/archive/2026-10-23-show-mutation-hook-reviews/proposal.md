## Why

Install, remove, and update can change executable hook declarations without showing the review listing that sync provides. Their TUI equivalents also lose the completed materialization report, so a later unchanged sync cannot recover the missing review.

## What Changes

- Preserve completed materialization reports through install, remove, and update.
- Show the existing hook review listing and materialization warnings in CLI mutation output, including Marketplace and collection installs.
- Retain completed hook review rows in the TUI diagnostics view and open that view when an operation changes hooks.
- Keep existing ownership, selection, acquisition, and exit-code behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-sync-diagnostics`: Extend completed hook review and warning reporting to resource mutation commands and their TUI equivalents.

## Impact

The shared pipeline results, CLI mutation output, TUI lifecycle completion, and real-terminal QA journeys change. Hook compilation, consumer configuration, acquisition locks, and ownership state formats remain unchanged. No dependency is added.
