## Why

The hook decoder must ignore the serializer's null `Extra` scaffolding so native
hook targets do not mistake it for an extension. Applying that normalization to
other kinds changes historical native output and breaks Marketplace TUI journeys.

## What Changes

- Limit null scaffolding normalization to hooks.
- Preserve non-hook canonical metadata and native output.
- Prove the boundary with decoder regressions and real-terminal journeys.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: preserve non-hook canonical metadata during hook decoding fixes.

## Impact

Canonical artifact decoding only; no configuration, lock or artifact identity format change.
