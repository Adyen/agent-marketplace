## Why

Valid requirement renames followed by modifications currently fail scenario traceability: the audit retains the old requirement and appends its renamed replacement, reporting duplicate scenario slugs.

## What Changes

- Apply requirement rename mappings before other delta operations, regardless of section order.
- Preserve scenario identities for rename-only deltas and resolve modifications and removals against the renamed requirement.
- Add regression coverage at the traceability boundary.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `spec-governance`: Traceability honors requirement renames in active changes.

## Impact

Only `tools/specaudit` and its governance contract change. No product runtime behavior or dependencies change.
