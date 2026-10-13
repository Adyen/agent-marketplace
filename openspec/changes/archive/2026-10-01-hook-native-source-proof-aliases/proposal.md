## Why

Several selected hooks can produce the same native hook entry. Ownership state currently records one resource name and source-hook fingerprint per entry, so a legacy-writing client can prove only one contributor. It can recreate another contributor in a legacy file even when the shared native entry is clean and already supplies that hook.

The native-location tolerance reader needs to recognize every recorded contributor before native hook migration is released.

## What Changes

- Allow a hook ownership entry to carry optional resource-name and source-fingerprint evidence tuples while retaining its existing scalar fields and ownership-state version.
- Match a selected hook against either the scalar evidence or one of the additional tuples, but only when every matching owned native entry is present and clean.
- Reject malformed, ambiguous, or non-hook source evidence without using it as proof.
- Keep compatibility checks read-only and keep hook payloads out of ownership state, diagnostics, and logs.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-native-location-tolerance`: recognize multiple contributing selected sources for one owned native entry without weakening current-byte ownership proof.

## Impact

- `internal/ownership`: optional hook source-evidence data and a proof accessor.
- `internal/materialize`: contributor-aware native hook satisfaction.
- Materialization and command tests for legacy scalar compatibility, multiple contributors, incomplete proof, malformed evidence, and read-only behavior.
- No source-revision schema or generator changes, native hook writer, location migration, handwritten adoption, or trust changes.
