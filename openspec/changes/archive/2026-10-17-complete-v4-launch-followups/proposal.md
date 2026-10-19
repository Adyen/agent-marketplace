## Why

Read-only acquisition checks stop at the first writable scope with an
unrecorded Marketplace endpoint registration. Users must repeat the command
to discover the remaining required changes. Historical adoption guidance
also duplicates current documentation and advertises unsupported formats.

## What Changes

- Report every selected writable scope with an unrecorded resource-free
  Marketplace endpoint registration in one `lock --check` invocation.
- Preserve diagnostic codes, scope selection, read-only boundaries, and
  existing locked-mode refusals.
- Remove the historical adoption guide after accounting for its current
  contracts and discarding obsolete or explanatory claims.
- Repoint surviving references to current public documentation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployed-acquisition-locks`: aggregate selected registration check failures.

## Impact

Changes command diagnostics and tests, the public CLI reference, and the
documentation harvest ledger. Logging and telemetry baselines in the same
pull request retire the other two historical documents without runtime changes.
