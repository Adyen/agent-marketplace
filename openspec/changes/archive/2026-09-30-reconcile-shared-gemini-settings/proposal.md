## Why

Gemini repository and machine scopes can resolve to the same settings file. Reconciling it independently drops entries that remain desired in the other scope.

## What Changes

- Combine desired native Gemini MCP servers and hooks before reconciling a shared destination once.
- Refuse conflicting keyed values and scope-restricted reconciliation of a shared root before writes.
- Retain handwritten bytes, modes, no-op behavior and incomplete-compilation protection.

## Capabilities

### New Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: Gemini native entries are reconciled against the combined desired scopes when their destination is shared.

## Impact

Native Gemini synchronization and check-only inspection. Distinct repository and machine destinations, plugin compilation and configuration/lock formats are unchanged. A scope-restricted operation on a shared Gemini root refuses rather than prune another scope.
