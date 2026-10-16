## Why

Normative behavior still lives in legacy documents outside `openspec/specs/`. Until each claim is either proven into a spec or discarded, contributors cannot tell which source governs. This change is the ledger that tracks that migration to completion.

## What Changes

- Tracks one task per legacy document and per non-normative `AGENTS.md` appendix section, each mapped to the capabilities expected to absorb it.
- Each `baseline-<capability>` change checks off the rows it absorbs and deletes those files in its own pull request; no document is deleted before its claims are harvested or explicitly discarded.
- The final pull request deletes any residue, repoints remaining links, and archives this change.
- Consumer configuration claims must satisfy `consumer-config-format`: harvest only v4 sources/resources behavior and unsupported-format refusal. Discard legacy consumer reads, edits, implicit selection, and in-process migration claims; do not recreate a `config-migration` capability. Independent lock, source-revision, ownership, native harness, and operator compatibility remain eligible for harvest.
- **BREAKING (documentation)**: `docs/`, `pkg/harness-compilation/CHANGELOG.md`, `pkg/harness-compilation/docs/`, `pkg/harness-compilation/diag/codes.md`, and `pkg/harness-compilation/field-matrix.md` are removed by the end of this change.

## Capabilities

### New Capabilities

None. This change carries no spec delta (`skip_specs: true`); the capabilities are introduced by the baseline changes that absorb each document.

### Modified Capabilities

None.

## Impact

- Documentation only; no runtime behavior changes.
- `README.md`, `site/docs/`, `CODING_STANDARDS.md`, `skills/`, and review-bot configuration are kept.
