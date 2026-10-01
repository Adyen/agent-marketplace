## Why

Issue #448 reports that `status` lists every Marketplace-sourced selection as orphaned with pending removals while `sync --check` passes for the same tree, and that `doctor` reports a prerequisite as unmet when only a Marketplace selection satisfies it.

## What Changes

- `status` includes Marketplace selections from every configuration layer in its synced-resource and drift inspection, resolving them from the verified local cache so its verdict agrees with `sync --check`.
- A selected Marketplace pin whose verified local artifact is missing is reported as an attributable error naming the pin and the acquisition remedy, without listing its outputs as orphaned.
- `doctor` validates prerequisites against a resolved set that includes Marketplace selections.
- Inspecting Marketplace selections downloads no resource artifacts and writes no configuration or lock files; selection, materialization, and the other commands are unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `status-reporting`: status includes Marketplace selections in its synced-resource and drift inspection from the verified cache, consistently with `sync --check`.
- `resource-prerequisites`: a prerequisite is met by a Marketplace-selected resource in the same resolution.

## Impact

The status and doctor command output, the inspect-mode Marketplace resolution, and their tests change. Selection precedence, materialization, lock format, and every other command are unchanged.
