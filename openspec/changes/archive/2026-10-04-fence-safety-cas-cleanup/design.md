## Context

The actual writer has producer payload state and published sidecar identity, but CAS cleanup discards them and unlinks two pathnames. Generation authority must originate at production, not be freshly admitted after refusal.

## Decisions

Return private safetyAttempt containing original relative safety scope/parent and both actual produced member generations. Apply's in-memory Journal carries it; JSON and restart recovery remain unchanged. Before removing anything recapture both original member states under captured original parent, then reuse shared named-parent/full-state RemoveLeaf for each. Preserve the original CAS/policy error and existing warning boundary if cleanup refuses. Empty cleanup compares original directory object and uses Rmdir, treating nonempty as a harmless no-op. Current recovery retains backups, never derives deletion authority from durable BackupPath. No atomic pair/overall transaction or uncooperative-writer exclusion. Existing cleanup comment updated accurately without adding/removing comments.

## Verification

Actual Apply after-staged-write replaces its produced payload, late policy rejects: parent deletes replacement/red; guarded cleanup retains replacement/original/paired metadata and live prior bytes while stage/journal cleanup still occurs/green. Healthy late refusal really removes original payload/sidecar/empty directory/stage/journal. Full Linux/macOS ownership/materialize/count20 CAS/crash races/validate/lint/build/strict specs pass. All eleven implementation files reviewed incl test/benchmark/script/spec exclusions, none skipped; private JSON omission, actual producer identity propagation, full selected generations, original error and empty-only cleanup checked. Real disk Apply→late CAS refusal→cleanup lifecycle includes payload/sidecar/journal/stage sync+removal, logdiscard/no network:47.078941→85.897886ms,158189→185038B,2079→2507alloc,five2s/no95%CI with validation-load variation. Safety cost, not wall-time budget/speedup.
