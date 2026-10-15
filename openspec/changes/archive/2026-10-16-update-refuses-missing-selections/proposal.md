## Why

When a selected resource is renamed or removed upstream, `agent-marketplace update` drops the selection without saying so; the warning only appears at the next `sync` or `status`. When the missing selection is required, `update` advances the acquisition lock, fails the following sync with `dependency-committed sync failure`, and leaves harness outputs behind the lock, so `sync --check` reports drift. Users need `update` to either apply a fully resolvable revision or change nothing.

## What Changes

- `update` checks every explicit selection against the new revision before it publishes any lock or config change. A selection counts when it is a direct selection, a `required: true` entry, a selected collection, or a member that the new collection definition still lists.
- If a selection that resolved at the previously locked revision no longer resolves at the new one, `update` exits 1, names each such selection once with the existing "was selected but not found" wording, prints one hint pointing to `sync --prune-stale` and `install`, and leaves acquisition locks, configuration, and harness outputs unchanged.
- Selections that already did not resolve before `update`, `all: true` selections that gain or lose names, and members dropped from a collection definition do not cause a refusal.
- Out of scope: Marketplace pin 404 handling, organization lock relocking, `sync`/`status`/`sync --prune-stale` behavior, automatic pruning, and collection selector-edge preservation.

## Capabilities

### New Capabilities

### Modified Capabilities

- `resource-selection`: adds the requirement that `update` refuses atomically when an explicit selection stops resolving at the new revision.

## Impact

- `cmd/agent-marketplace` update and lock lifecycle: a selection check runs between snapshot acquisition and dependency publication.
- `internal/pipeline` update flow: the refusal surfaces as the command error instead of a dependency-committed sync failure.
- No lock or configuration format change.
