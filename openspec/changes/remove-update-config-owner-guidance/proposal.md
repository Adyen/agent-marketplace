## Why

The update output tells consumers to ask their organization config owner to update Marketplace pins even though the command has not checked those pins for newer revisions. Remove that unsolicited guidance.

## What Changes

- Remove the organization config owner instruction from default and verbose update output.
- Keep the organization-managed pin summary, verbose listing, and update behavior unchanged.
- Remove the instruction and corresponding owner-update workflow from the CLI reference.

## Capabilities

### New Capabilities

- `update-output`: organization-managed Marketplace pin reporting without config owner guidance.

### Modified Capabilities

None.

## Impact

Only update output, its regression tests, and the CLI reference change. Configuration, acquisition locks, and materialized resources retain their existing behavior.
