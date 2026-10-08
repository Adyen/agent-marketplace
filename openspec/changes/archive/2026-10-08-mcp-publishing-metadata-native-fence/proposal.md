## Why

Released clients preserve unknown MCP server fields and copy `_agent-marketplace` publishing metadata into native connection settings. Reserving that namespace at the native boundary is a compatibility prerequisite for publishing descriptions and documentation.

## What Changes

- Exclude `_agent-marketplace` and case variants from MCP native output for all seven harness targets, without mutating intrinsic resource content.
- Preserve native connection settings and established handling of unrelated extensions, with no unsupported-field warning caused solely by publishing metadata.
- Prove the boundary through canonical artifact restoration, public compilation/materialization, and actual synchronization.
- Leave canonical encoding, historical source revisions, healthy acquisition locks, and writer defaults unchanged.

## Capabilities

### New Capabilities

- `mcp-publishing-metadata-native-fence`: Publishing namespace exclusion at native MCP serialization while preserving intrinsic content and existing synchronization contracts.

### Modified Capabilities

None.

## Impact

MCP serializers and their shared extra-field selection, focused consumer-path tests, and native-boundary documentation. This does not implement authoring validation, descriptions, README loading, public inspection, UI presentation, a new source-revision format, or a release. Future committed formats still require their own released-reader proof.
