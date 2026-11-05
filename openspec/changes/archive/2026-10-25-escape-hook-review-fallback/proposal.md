## Why

Hook reviews escape command and prompt strings, but their compact JSON fallback can emit non-printable Unicode from other handler fields directly to the terminal.

## What Changes

- Escape terminal controls in fallback hook review payloads while preserving printable JSON syntax and values.
- Prove the behavior through the shared review writer, a CLI mutation, and a real-terminal HTTP hook journey.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-sync-diagnostics`: Require terminal-safe compact JSON fallback reviews.

## Impact

Review presentation changes. Materialization, owned entries, configuration formats, and command success criteria are unchanged.
