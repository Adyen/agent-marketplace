## Why

Installing related resources currently requires separate commands. The install command rejects multiple names and the shared pipeline only uses the first name.

## What Changes

- Accept multiple resource names from configured registries or one `--from` Git or directory source, applying shared flags to every name.
- Resolve the batch before publishing configuration and acquisition lock changes, then synchronize once.
- Deduplicate repeated names in request order and reject invalid batches without publishing selections or materialized output.
- Preserve existing single-resource, collection, and no-name `--from` behavior. Marketplace resource locators and endpoint registration remain singleton operations.

## Capabilities

### New Capabilities

- `resource-installation`: Named batch selection, failure safety, and compatibility with existing install modes.

### Modified Capabilities

None.

## Impact

The Cobra install argument validation and shared pipeline install selection change. Command and E2E tests cover the new batch boundary; the CLI reference changes its usage and examples. Configuration schemas, flags, Marketplace acquisition, TUI behavior, and harness translation remain unchanged.
