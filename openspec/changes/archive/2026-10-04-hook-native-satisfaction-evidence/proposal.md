## Why

Native hook reconciliation must let an identical handwritten entry satisfy a selected hook without adopting it. The legacy reader currently requires owned-entry proof, so removing a marked legacy twin can cause that reader to recreate legacy output even though the selected behavior remains present in the native file.

## What Changes

- Recognize separate, source-bound native satisfaction evidence for handwritten hook entries, without treating those entries as owned.
- Require exact selected-resource and version-1 source-fingerprint matches and complete current canonical native-value proof before suppressing a legacy write.
- Keep owned-entry classification, pruning, existing scalar and multi-source ownership proof, ownership-state version 1, and source-revision generators v1 through v4 unchanged.
- Fail closed on malformed satisfaction evidence and preserve native, legacy, and state files during read-only inspection.
- Publish this bounded reader extension in the existing tolerance pull request before native migration is released. Native writer activation and migration remain separate work.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-native-location-tolerance`: source-bound satisfaction proof can suppress legacy output without adopting a handwritten native entry; incomplete or unsafe satisfaction proof cannot do so.

## Impact

- `internal/ownership`: optional satisfaction records, non-secret validation, and read-only proof accessors separate from owned-entry APIs.
- `internal/materialize`: complete source-and-current-value matching across owned and satisfaction evidence.
- Command tests: real `sync`, `sync --locked`, and `sync --check` witnesses for the four legacy-writing harness targets in both scopes, with mismatched, partial, malformed, and no-write controls.
- No plugins, trust changes, native writer switch, migration, support-script rewriting, source fingerprint algorithm changes, state-version change, or new source-revision version.
