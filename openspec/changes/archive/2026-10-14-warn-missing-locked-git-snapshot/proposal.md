## Why

A repository acquisition lock can advance before a consumer has cached its exact snapshot. The TUI currently presents this ordinary recoverable cache miss as an error without directing the user to sync.

## What Changes

- Present missing locked exact snapshots as TUI inspection warnings with the sync shortcut and `agent-marketplace sync --locked` remediation.
- Keep affected selections and drift unverified until explicit synchronization succeeds, without automatic acquisition or mutable-ref fallback.
- Keep corrupt snapshots, acquisition failures, command checks, and unsuccessful operations as failures.
- Make the sync recovery shortcut visible at supported terminal widths and verify recovery through a real-terminal journey.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-feedback`: Recoverable missing-exact-snapshot warnings and discoverable synchronization recovery.

## Impact

TUI snapshot diagnostics, footer hints, regression tests, real-terminal QA, and the existing TUI reference. No configuration, acquisition-lock, ownership-state, or wire-format change; no new dependencies.
