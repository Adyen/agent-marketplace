## Why

Separating authored configuration from deployed acquisition evidence must still fail closed when a deployed lock is stale or malformed, while permitting absent organization lock deployments.

## What Changes

- Inspect deployed acquisition locks only for layers that require them.
- Keep missing writable locks and malformed or stale deployed organization locks distinct.
- Acquire verified evidence when an online operation needs it without treating placeholder locks as deployed authority.

## Capabilities

### New Capabilities

- `deployed-acquisition-locks`: Which deployed lock evidence binds remote resources and when acquisition is permitted.

### Modified Capabilities

None.

## Impact

Lock operations, effective config binding, online acquisition, and organization policy enforcement.
