## Why

Configured bulk installs reject user and personal-local scope even though the layer editor, dependency transaction, and materialization pipeline already support those destinations. Users should be able to choose a writable scope without installing each matching resource individually.

## What Changes

- Support `install --tag` and `install --all` with `--scope user`, `--scope repo`, and `--scope local`.
- Preserve tag selectors, explicit snapshot selection for `--all`, resource-kind and registry filters, exact acquisition evidence, and existing scope safety checks.
- Document the supported scopes and prove scoped configuration, acquisition lock, and materialization outcomes.

## Capabilities

### Modified Capabilities

- `resource-installation`: configured bulk installation across writable scopes.

## Impact

The shared install pipeline and CLI reference change. No consumer configuration or acquisition lock format changes are required.
