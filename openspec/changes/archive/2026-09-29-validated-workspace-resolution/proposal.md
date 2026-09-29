## Why

Commands could discover different repository, personal-local, and output roots in one invocation. An invalid nested root could also reach lock preparation or filesystem mutation before being rejected.

## What Changes

- Resolve the repository and personal-local roots once for an invocation, and use the personal-local root for nested harness output.
- Refuse roots outside the repository, symlink escapes, and conflicting root overrides before reading configuration or changing output.
- Keep the committed repository configuration rooted at the repository even when invoked beneath a nested personal-local directory.

## Capabilities

### New Capabilities

- `workspace-resolution`: Root discovery, containment, and the relationship between nested personal-local configuration and generated output.

### Modified Capabilities

None.

## Impact

CLI commands that discover configuration, resolution and synchronization, nested personal-local installations, and filesystem containment tests.
