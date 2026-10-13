## Why

The resolver selects or previews prerequisites, but existing CLI tables and install summaries do not show those relationships. Consumers need to distinguish an implied selection, an advisory preview and an unmet requirement without inspecting configuration.

## What Changes

- Add install-associated transitive prerequisite selection or advisory-preview summaries after existing install output.
- Add requirement and collection provenance to configured list and status output, and declared requirements to list --from.
- Add typed unmet reasons to status and an explicit warning-only doctor check for unconfigured hints.
- Preserve selection, acquisition, strict exit behavior and the disabled expansion default.
- Reconcile affected published CLI and prerequisite guidance.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-installation`: report implied and preview prerequisites associated with an install.
- `resource-prerequisites`: expose declared requirements, typed status reasons and unconfigured-hint diagnostics.
- `status-reporting`: show selected requirement provenance alongside collection provenance.

## Impact

CLI install, list, status and doctor output; minimal resolver provenance and pipeline result plumbing; focused behavioral command tests; prerequisite and CLI documentation. No new public command or flag, configuration or acquisition-lock format, Marketplace graph, TUI or web change.
