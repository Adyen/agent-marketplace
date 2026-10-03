## Context

See proposal.md for the compatibility gap. The current reader takes source evidence only from `Proof.OwnedIDs()` and requires `StatusOwnedClean`. The native JSON reconciler deliberately leaves an identical handwritten entry unmanaged. These decisions are individually correct but cannot prevent legacy recreation after a marked legacy twin is removed.

The existing state has version 1, per-file owned `Entries`, scalar hook `SourceDigest`, optional contributing `SourceEvidence`, strict typed JSON decoding, and separate canonical native-value inspection. The reader worktree starts from the already reviewed scalar/multi-source reader. Source fingerprint algorithms and their frozen vectors are unchanged.

## Goals / Non-Goals

**Goals:**

- Recognize exact source-bound native satisfaction without conferring ownership.
- Preserve current-byte, target, scope, resource, source-revision, and no-write boundaries.
- Reuse the existing native inspector and source fingerprint derivation rather than comparing commands, names, or old/new compiler output.

**Non-Goals:**

- Native writer activation, legacy migration, support routing, satisfaction production during sync, or a metadata-only mutation API.
- Changing owned-entry APIs, pruning policy, acquisition locks, source-revision generators, state version, trust, plugins, or script contents.
- Inferring satisfaction from a handwritten native entry that has no durable source evidence.

## Decisions

### D1: A distinct per-file satisfaction collection

Add an optional `hook_satisfactions` collection to `ownership.FileState`, separate from `entries`. Its element is:

```go
type HookSatisfaction struct {
    Container      string               `json:"container"`
    IdentityDigest string               `json:"identity_digest"`
    ValueDigest    string               `json:"value_digest"`
    Sources        []HookSourceEvidence `json:"sources"`
}
```

`Container` is the native event. `ValueDigest` is the canonical native rule digest. `IdentityDigest` must equal the existing total `HookIdentityDigest(Container, ValueDigest)`. Every source tuple carries the installed resource name and its existing version-1 source fingerprint.

A satisfaction record has no ownership kind or MCP key. It is not inserted into the owned-entry map. `Proof.Classify`, `OwnedIDs`, `OwnedCount`, `Resource`, `SourceDigest`, and existing `HasHookSource` retain their current owned-only meaning.

Expose `HookSatisfaction.ID() EntryID` for native identity lookup and `Proof.HookSatisfactions() []HookSatisfaction` as a deterministic, deep-copied read-only view. A caller mutating that returned slice or its nested sources must not modify subsequent proof results or classification.

Alternative rejected: put a satisfaction flag on an owned `Entry`. Existing consumers would still encounter that entry through ownership APIs, risking adoption or pruning. A separate collection preserves the authority boundary structurally.

### D2: Strict validation without read-side repair

Validate satisfaction records during state loading. Require safe event/resource tokens, valid algorithm-qualified SHA-256 identity/value/source digests, the exact event/value identity relationship, at least one source tuple, unique native identities per file, and unique resource/fingerprint pairs per record. A field value must never appear in an error or log. Existing typed JSON decoding must reject exact duplicates and case-colliding aliases on the new modeled collection, records, and source tuples before decoding discards ambiguity.

Do not normalize or synthesize satisfaction identities while reading. The existing hook/v1-to-hook/v2 normalization applies only to historical owned entries. Absent satisfaction fields decode as an empty collection and do not trigger state writes.

This reader change does not add a producer or alter the public mutation shape. Tests seed exact version-1 state fixtures directly, as the existing malformed-evidence fixtures do. A later native writer change must publish the satisfaction collection transactionally with its own native state and preserve owned records in shared files.

### D3: Complete proof over both evidence classes

Retain the existing owned-entry loop in `nativeHookInspection.hookSatisfied`. Then inspect the separate satisfaction collection. For each record carrying the exact requested resource and source fingerprint, require that its native identity is currently present and its canonical value digest equals the record's digest. Any matching missing or changed record returns false, even when another matching owned or satisfaction record is clean. Return true only if at least one matching record was checked and every matching record is current.

Satisfaction-only records do not call ownership classification to establish a grant. Current-value equality proves only that the selected behavior remains present. `InspectNativeHooks` continues to expose the entry as unmanaged; its owned-source digest field remains empty for satisfaction-only evidence.

This reuses the descriptor-anchored native inspector, strict Cursor envelope check, canonical rule identity, and existing source fingerprint function. No matcher table, source-name fallback, or runtime legacy translator is introduced.

### D4: Reader behavior remains non-mutating at native/state boundaries

For a complete match, continue using the existing all-or-nothing suppression and surviving marked legacy twin preservation. The compatibility reader does not populate satisfaction evidence. In Windsurf machine scope, the same physical hook file can contain a satisfied handwritten entry and unrelated legacy siblings; preserve the satisfied bytes and order while normal reconciliation remains available to unproven selections.

Mismatched or partial evidence suppresses nothing. Invalid evidence uses the existing state-corrupt/native-tolerance refusal path, scoped to the affected target and scope. Read-only checks preserve all file bytes, modes, and modification times and create no state or lock files.

### D5: Owning-boundary and real-command tests

Ownership tests cover satisfaction-only classification, empty owned IDs, deep-copy accessors, old scalar and tuple records, invalid identities/tokens/digests, missing source pairs, duplicate identities/pairs, wrong JSON member types, duplicate keys, and case aliases. Expected state data is independently authored, not regenerated from a new writer.

Materialization tests seed actual native documents for Claude Code, Cursor, Factory Droid, and Windsurf in repository and machine scopes. Exercise exact satisfaction, no-evidence positive controls, resource/fingerprint mismatches, crossed contributor pairs, two contributors sharing an entry, and partial mixed owned/satisfaction proof. Assert unchanged native/state bytes, modes, and modification times; retain existing exact legacy preservation tests and independent-target controls.

Command tests use the real isolated CLI command path for `sync`, `sync --locked`, and `sync --check` across the same target/scope matrix. Native and state fixtures must describe actual matching handler behavior. Test incomplete, corrupt, wrong-scope, and absent-state controls without accepting an unrelated failure as proof.

## Risks / Trade-offs

- Satisfaction mistaken for ownership → separate collection and unchanged ownership APIs; explicit unmanaged and deselection/cleanup controls.
- A clean record hides a missing sibling record → check every matching record across both classes before suppressing anything.
- Reading silently repairs unsafe metadata → strict identity validation and raw state byte/mode/mtime assertions.
- A fixture proves only a helper → actual materialization and isolated CLI witnesses in addition to pure state tests.
- Historical clients predating this reader recreate output → release the updated tolerance reader before native activation; no compatibility waiver.

## Migration Plan

1. Publish read support and its spec in the existing tolerance pull request, with fresh verification and independent reviews.
2. Release that reader before native migration is published.
3. Import the reviewed reader into the native integration branch and separately implement satisfaction production and transactional native activation.

Rollback to a client predating this reader loses satisfaction recognition but does not acquire ownership of a handwritten entry. Do not roll back to such a client in a repository that relies on native migration.
