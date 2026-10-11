## Why

Orphan removal re-nominates under the write session, but the authorizing metadata can be replaced during staging or policy validation before Apply publishes deletion. The admitted evidence generation is not currently retained to that action boundary.

## What Changes

- Carry private source directory/file pins from opened safety manifests and journals into evidence-backed candidates without changing JSON.
- Keep the fresh matching candidate admitted inside RemoveOrphan's write session.
- Revalidate the authorizing metadata after existing policy validation and before native publication, reusing captured metadata reads and CAS refusal cleanup.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: orphan evidence-action generation binding.

## Impact

R2 child of #527. Actual Devin JSON/Codex TOML removal consumers; late external alias and byte-identical replacement refuse and valid restored authority still performs real backed-up deletion. No new budgets, consent semantics, batch transaction or uncooperative-writer exclusion. Restart recovery and final admitted-case audit remain separate.
