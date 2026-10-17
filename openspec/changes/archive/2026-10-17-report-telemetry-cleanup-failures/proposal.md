## Why

Diagnostic completion and installation-ID reset currently ignore failures to
remove retained diagnostic evidence, so they can report success while
sensitive local data remains.

## What Changes

- Report review-payload deletion errors after accepted diagnostic delivery.
- Propagate retained-diagnostic cleanup errors during installation-ID reset.
- Preserve the underlying filesystem error, disclose partial completion, and
  retain the investigated command's independent outcome.
- Prove both cleanup refusals and healthy/retry controls with real filesystem
  failures, and strengthen the reviewed lock-diagnostic and logging tests.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `telemetry`: successful completion requires local evidence cleanup.

## Impact

Changes telemetry cleanup error handling and test evidence. No new
configuration, transport, acquisition, or recovery mechanism is introduced.
