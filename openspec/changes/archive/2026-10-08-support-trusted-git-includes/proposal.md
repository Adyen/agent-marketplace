## Why

HTTPS registry acquisition currently refuses any Git configuration include, including company-managed system configuration that delegates authentication to a user-owned file. Validated ordinary include chains should work without admitting repository-local authentication or executing unrelated Git configuration.

## What Changes

- Expand ordinary `[include] path` entries recursively from system and global Git configuration.
- Validate every opened file in the chain, preserve declaration order and credential-helper resets, and bound file size, total expansion, and nesting.
- Preserve symlinked regular configuration files, relative paths, and `~/` paths.
- Continue refusing conditional includes and repository-local authentication.
- Document the supported paths and trust failures.

## Capabilities

### New Capabilities

- `git-authentication`: trusted system/global authentication configuration and ordinary include chains for HTTP(S) registry acquisition.

### Modified Capabilities

None.

## Impact

The shared authentication reader in `internal/cache`, its acquisition tests and benchmarks, and the private-registry and environment-variable documentation. No configuration or acquisition lock schema changes, new dependencies, or native harness changes.
