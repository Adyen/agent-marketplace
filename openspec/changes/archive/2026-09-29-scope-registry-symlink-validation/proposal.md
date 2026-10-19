## Why

A broken or escaping symlink elsewhere in a registry repository can block acquisition of an otherwise usable exact snapshot. When a symlink inside the configured source root is unsafe, an opaque cache failure does not tell the user which registry path to fix.

## What Changes

- Allow an exact snapshot to be acquired and reused when its tracked tree contains dangling or escaping symlinks outside the configured source root, without weakening exact tracked-tree integrity checks.
- Validate symlink containment within the configured source root when building its source revision; reject unresolved links or links resolving outside that root.
- Identify a rejected link by registry-relative path, authored target, configured root, and actionable remediation.

## Capabilities

### New Capabilities

- `registry-symlink-containment`: Boundary between exact snapshot acquisition and validation of the configured source root, including actionable unsafe-link diagnostics.

### Modified Capabilities

None.

## Impact

Git-backed registry acquisition and source-revision building; the existing exact snapshot trust and source path escape error identities remain unchanged. No configuration, lockfile, or materialized output schema changes.
