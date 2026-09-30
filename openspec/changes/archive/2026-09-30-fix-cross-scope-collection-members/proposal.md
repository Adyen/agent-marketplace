## Why

Per-scope collision resolution covers resources selected directly, but not resources selected through a collection. A repository selection can still remove a same-named collection member from the machine harness target, so repository and user syncs disagree and `sync --check` passes in only one place. When the collection belongs to a required source, the required machine resource disappears without a warning. Separately, whether the repository keeps its copy from the required source depends on how an unrelated competing source location sorts.

## What Changes

- Resolve collection members per install scope. A selection in one scope no longer displaces a collection member in the other scope.
- Apply the required-source rule to resources selected through collections. A non-required repository resource from another source cannot shadow a required machine-scope resource, whether either side is selected directly or through a collection.
- Remove repository selections that would shadow a required resource before the repository-scope winner is chosen. A repository copy from the required source then remains installed regardless of how the source locations sort.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-selection`: per-scope collision resolution and required suppression now cover collection-selected resources, and required suppression no longer depends on source location order.

## Impact

Resource resolution and its regression tests change: resolver unit tests, a command test that syncs in both orders, and declarative E2E cases. Configuration, lockfile, and diagnostic formats do not change.
