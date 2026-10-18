## Why

Mirrored hooks must deliver executable support without restoring superseded
native installation paths or treating one unsafe hook as a registry-wide failure.
Current native installation and captured mirror inventories provide the boundary.

## What Changes

- Package adjacent hook assets at plugin-relative paths, binding bytes and modes.
- Validate captured support inventories at resolution and materialization.
- Skip an unsafe hook with a diagnostic while publishing valid siblings.
- Preserve nested shell quote contexts and ownership refusal behavior.
- Prove missing support drift through the real Marketplace consumer.

## Capabilities

### Modified Capabilities
- `marketplace-api`: verified mirrored hook support and partial publication.

## Impact

Marketplace ingestion, mirror resolution, native hook materialization, shell
translation, and their regression coverage. No consumer config or lock changes.
