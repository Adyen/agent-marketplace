## Decision

Keep canonical decoding unchanged and recognize exactly `Extra: null` in
OpenCode's strict hook admission check. Additional, differently named or non-null
metadata remains unsupported. The check must not mutate the shared IR resource.

This supersedes the decoder normalization approach: even hook-only removal
changes Factory's historical native bytes. Do not weaken native ownership
comparison or change fixtures to conceal that drift.

## Verification

Exercise real canonical encode/decode and OpenCode translation, with unknown
metadata refusal controls. Retain the existing Marketplace-native ownership
test unchanged; it independently proves native bytes and mtimes survive sync
and check mode.
