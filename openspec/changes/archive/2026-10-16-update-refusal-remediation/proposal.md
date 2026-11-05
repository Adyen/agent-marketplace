## Why

The refusal added by `update-refuses-missing-selections` tells users to run `sync --prune-stale` and then `install <new-name>`. With the lock unchanged, neither command recovers: `sync --prune-stale` prunes nothing because the old name still resolves at the old lock, and `install <new-name>` cannot find the renamed skill in the configured registries. The working path is `remove` of the old selection, then `update`, then `install <new-name>`. The refusal also discards errors from loading the previously locked revision, so selections that were already stale are reported as newly missing with no explanation, and it does not say that `update --registry <url>` can update unaffected registries.

## What Changes

- The refusal prints one remediation line after each named miss. For a selection that is not required, the line names `agent-marketplace remove --kind <kind> <name>` to run before rerunning `update`, and `agent-marketplace install <new-name>` for a renamed resource. For a required selection, the line states that `agent-marketplace remove` cannot drop it and that the configuration entry marking it required must change.
- When the previously locked revision of a lock cannot be loaded, the refusal names that lock and the load error. Selections that could not be compared still count as newly missing.
- The closing line names `update --registry <url>` as the way to update other registries while a selection stays unresolved.
- Out of scope: new CLI flags, changes to `sync --prune-stale`, `install`, or `remove` behavior, and automatic pruning.

## Capabilities

### New Capabilities

### Modified Capabilities

- `resource-selection`: replaces the update refusal requirement so its remediation is followable, adds prior-revision load failures to the refusal, and documents the targeted registry escape.

## Impact

- `cmd/agent-marketplace` update selection guard and refusal text.
- Command tests in `update_selection_refusal_test.go`.
- No lock or configuration format change.
