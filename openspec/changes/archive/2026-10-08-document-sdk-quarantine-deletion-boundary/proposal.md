## Why

SDK stale-file pruning verifies a quarantined file and then deletes its name in a separate operation. Existing checks preserve replacements they observe, but do not establish atomic identity-conditional deletion against a hostile same-account process. The specification must not promise more than the supported implementation guarantees.

## What Changes

- Explicitly accept deliberate same-UID interference in the final quarantine verification/deletion window as outside the supported threat model.
- Scope the existing replacement-preservation scenario to replacement before final verification, retaining its current test binding.
- Record the residual data-loss risk, alternatives, and conditions for revisiting the decision in an accepted ADR.
- Preserve all current runtime checks, error identities, removal reporting, and cleanup/recovery behavior.

## Capabilities

### Modified Capabilities

- `materialization`: clarify the supported quarantine deletion guarantee and its narrow non-goal.

## Impact

Documentation and specification clarification only. No implementation, API, dependency, platform, or performance change. Existing preservation and healthy-pruning controls remain applicable; they are not evidence of atomic deletion.
