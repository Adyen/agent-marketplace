## Why

When the same registry location appears in several consumer configuration layers, an unqualified collection member can inherit a different layer than the collection that selected it. This moves repository resources into machine scope, collapses independently selected copies, and can strip required policy or compatible harness targets.

## What Changes

- Preserve the resolved collection owner's location, configuration layer, required policy, and effective harness targets for members without an explicit registry.
- Preserve independent machine and repository copies when both scopes select the same collection.
- Keep explicit cross-registry member binding and nested collection definition precedence unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-selection`: unqualified collection members retain their resolved collection owner's scope and policy.

## Impact

The shared resolver and regression tests change. Consumer configuration, acquisition lock, and diagnostic formats do not change.
