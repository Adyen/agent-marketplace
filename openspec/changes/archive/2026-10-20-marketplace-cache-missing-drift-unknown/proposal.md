## Why

`list` and `status` report false drift for installed Marketplace resources when the verified Marketplace cache is missing (GitHub issue 790). After `~/.cache/agent-marketplace/marketplace` is deleted, `list` prints `1 resources out of sync` and `status` counts the pin's outputs as files to remove, while `sync --check` re-acquires the pinned artifacts and exits 0.

Both commands resolve Marketplace selections from the verified cache only. A pin whose revision metadata or collection manifest is absent from the cache becomes a soft warning and contributes no desired artifacts, so the dry-run drift probe sees the pin's owned outputs as no longer desired. Ownership state records resource names but not the pin that produced them, and a collection's members are unknown without its manifest, so the outputs of one pin cannot be singled out.

## What Changes

- When any selected Marketplace pin cannot be resolved from verified cache evidence, `list` reports the sync state as unknown with the `agent-marketplace sync` remedy instead of an out-of-sync count.
- In the same condition, `status` reports DRIFT as unknown, names the missing Marketplace cache evidence and the `agent-marketplace sync` remedy, and reports no orphaned files or pending removals. The soft never-acquired case keeps exit 0; the missing-artifact case keeps its nonzero exit.
- With complete cache evidence, drift reporting is unchanged.
- No change to `sync --check`, `doctor`, the TUI, ownership state, or lock formats.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-listing`: the sync-state line is unknown when Marketplace cache evidence is missing.
- `status-reporting`: DRIFT is unknown, without orphan rows, when Marketplace cache evidence is missing.
