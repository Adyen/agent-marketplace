## Why

A client released before native hook migration writes plugin-layout hook files. When a newer client moves owned hooks into native files, that older client may recreate the legacy entries, causing duplicate execution or disabling hooks in a harness that prefers the native file. The older client must learn to recognize proven native output before the migration ships.

## What Changes

- A legacy-writing client treats a selected hook's equivalent, ownership-proven entry in its target's native file as satisfying that hook. It does not recreate the legacy entry or report false drift on `sync`, `sync --locked`, or `sync --check`.
- Missing, different, unproven, malformed, or ambiguous native data never grants ownership or triggers native writes or prune. An affected target fails closed where it cannot safely determine equivalence; independent targets continue.
- This is read support only. The client continues to write legacy hook locations for hooks that are not satisfied by proven native entries.

## Capabilities

### New Capabilities

- `hook-native-location-tolerance`: Compatibility of a legacy-writing client with ownership-proven hook entries already installed in native harness files.

### Modified Capabilities

None.

## Impact

Legacy hook planning and aggregate reconciliation in `internal/materialize`, native file reading and ownership proof, focused CLI and E2E compatibility tests. There is no new native hook writer, hook translation, trust-state change, or legacy migration in this change.
