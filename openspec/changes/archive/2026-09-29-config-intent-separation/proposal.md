## Why

Loading layered configuration for inspection could also acquire remote resources or demand their locks. Parsing authored configuration must not depend on acquisition evidence.

## What Changes

- Keep validated, ordered source and resource intent available without lock acquisition.
- Bind exact remote resources only for operations that need them, preserving config precedence and error order.
- Preserve the provenance of parsed files and included fragments across later filesystem changes.

## Capabilities

### New Capabilities

- `configuration-intent`: Validated configuration reads and separate exact acquisition.

### Modified Capabilities

None.

## Impact

Layered configuration reads, includes, Marketplace requests, acquisition locks, and command consumers.
