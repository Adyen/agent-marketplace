## Why

When a resource is renamed or removed upstream, the Marketplace answers its latest-revision lookup with 404. Today `agent-marketplace update` stops at the first such pin with a bare `marketplace error 404: "resource not found"` and updates nothing, so one renamed resource blocks every other update and the user is not told what to do.

## What Changes

- `update` treats a 404 from a Marketplace pin's latest-revision lookup as a missing resource instead of aborting the run.
- Every other pin is planned, updated, locked, and synced as usual. A missing pin keeps its current pinned revision, and its lock entry is not changed.
- After writing the normal update report, `update` exits 1 with an error that names each missing resource and tells the user to remove the old pin and install the new name.
- When every checked pin is missing, nothing is staged or synced, and `update` still names the missing resources and exits 1.
- Other Marketplace failures, including a registry-level 404, still stop the run before anything changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `update-output`: Reports Marketplace resources that are no longer found, without blocking updates to other pins.

## Impact

Marketplace update planning in `internal/pipeline`, latest-revision error classification in `internal/marketplace`, and update reporting in `cmd/agent-marketplace`, with their regression tests. No configuration, lock, or Marketplace API format change; no TUI change; Git registry update behavior is unchanged.
