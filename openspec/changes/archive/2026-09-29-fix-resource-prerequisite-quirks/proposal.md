## Why

Registry authors can silently mistype `metadata.requires`: `all` is mistaken for a skill kind, and malformed items disappear from command output. CI also cannot enforce unmet prerequisites with `sync --strict`. Issue #401 tracks these quirks now that the current behavior is documented by the parent change.

## What Changes

- **BREAKING**: Reject `kind: all` and `all:name` as invalid prerequisite items rather than treating them as skill requirements; valid items in the same list survive.
- Show malformed `metadata.requires` items as stable, coded warnings with resource location in `list --from .` and `doctor`. Preserve valid items, avoid printing sensitive input, and do not fail ordinary inspection or selection because of malformed advisory metadata.
- **BREAKING**: `sync --strict` prints unmet prerequisite warnings and exits nonzero. Plain `sync` remains advisory and no prerequisite is selected automatically.
- Add tests first, then update author/consumer docs and the OpenSpec contract inherited from the parent change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-prerequisites`: reject `all` prerequisite kinds; surface malformed declarations; make unmet prerequisites fail strict sync while keeping non-strict commands advisory.

## Impact

`internal/catalog` parsing and diagnostic propagation, `cmd/agent-marketplace` list/doctor/sync, tests in affected packages and E2E, and `site/docs/` prerequisite guidance. No Marketplace service, selection, or harness output changes. This PR targets `metadata-requires` (PR #402), whose archived baseline spec defines the previous behavior; it does not rewrite that archived historical change.
