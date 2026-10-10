## Context

See proposal.md for motivation and the two capability deltas for behavior. The current reader can prove native entries only from its own ownership state. Catalog-driven native projection exists in the subsequent native-writer branch, but the prerequisite must not import unrelated model policies, change default legacy writes or activate native migration.

## Goals / Non-Goals

**Goals:**

- One shared catalog-driven native projection for read-only compatibility and the subsequent native writer.
- Complete independent repository satisfaction from verified source and native/support bytes, with no private ownership-state transfer.
- Preserve existing private proof, legacy twins, frozen acquisition locks, source fingerprints, no-write checks and target isolation.

**Non-Goals:**

- No source-revision or ownership-state version bump, adoption, pruning authority, signing keys or new trust subsystem.
- No default native writes, support-link conversion or native activation in the reader prerequisite.
- No model policy, skill/subagent translation, non-hook materialization or machine-scope behavior changes.
- No reads of arbitrary command paths or another client's HOME/cache.

## Decisions

### D1: Catalog dependencies are immutable reuse

Copy only the reviewed hookpolicy/toolpolicy runtime and their shipped catalogs. Add only hook provenance required to interpret native source values and the referenced diagnostic definitions. Do not import skill/subagent model or tool metadata. Default compiler golden output must remain unchanged until an explicit native projection is requested.

### D2: Explicit pure native projection

Provide `CompileNativeHookProjection(h *ir.Hook, dst Target, scope Scope, supportBase string) (*Result, error)` through the HCC boundary. It validates the resource, resolves event/matcher/type/timeout/environment through shared hookpolicy, serializes the exact native rule and declared envelope, and returns external-reconciliation fragments with private modes and no ownership marker. Catalog resolution remains authoritative; there is no matcher fallback or command-string-only shortcut.

The native writer uses this same projection rather than a separately reconstructed representation. The opt-in hook-only serializer uses shared catalog resolution and existing canonical field validation. Default `Compile` and legacy emitters remain unchanged because their paths and shapes are compatibility contracts. Reader and writer share the native serializer; neither reimplements matcher translation or command rewriting.

Native projection topology is additive to the authoritative target registry: `Definition` gains optional `HookProjection` metadata containing project/user native files, hook shape, envelope and support directory. Keep existing root models and default legacy NativeLayout Project/User declarations unchanged; several targets do not yet declare a complete NativeLayout, so adding one just for this hook seam would change unrelated routing. Validate and defensively copy the optional metadata at the existing registry boundary. Projection and the repository prune fence consume this metadata. The existing Claude, Cursor, Factory and Windsurf inspection table remains the reader eligibility boundary, including unchanged machine-scope behavior. Reconstruction cross-checks its resolved root, file, member and envelope against the projection declaration and establishes no match on disagreement. The native writer later consumes the same projection metadata.

### D3: Shared verified preparation

Use `prepareNativeHookProjection(w skillTarget, repoRoot string) (nativeHookProjection, error)` as the materialize boundary. The result contains exact native artifacts/entries, portable support artifacts, the stable source digest, declared output root and support namespace. Resolve installed name once and preserve deterministic contribution ordering.

Reuse the existing safe source fingerprint boundary and the reviewed source snapshot/support projection seam. Capture the definition and support together, re-ingest/select the exact hook from captured definition bytes when the base lacks private ingestion binding, and reject semantic disagreement with the selected resource. Do not fingerprint a different live definition after compiling stale IR. Verified mirror input uses its existing disjoint fingerprint and verified intrinsic hook/support artifact; unavailable or unverified input establishes no reconstruction match.

Derive the support namespace from the same stable source identity, installed name, target declaration and configured root as the native writer. The reader does not materialize or repair support. Repository support must be regular copies whose bytes and permission bits equal the projection.

### D4: Ephemeral complete-match result

Use `independentlySatisfiedNativeHook(w skillTarget, repoRoot string, ins *nativeHookInspection) (bool, error)` after existing inspection/refusal. It prepares the selected source projection and verifies native and support equivalence. Machine scope and missing native output establish no reconstruction match.

Existing selected-resource durable evidence remains authoritative: when any owned or satisfaction record carries evidence for that selected resource, apply the existing complete current-byte/source-fingerprint rule and do not fall back around contradictory, changed, incomplete or drifted proof. Existing malformed/corrupt proof errors refuse before reconstruction. A private satisfaction record covering an expected native identity also prevents fallback for an omitted resource association; reconstruction cannot complete a partial private record.

For a selection without such private evidence, compare every projected event/entry identity and complete canonical value with current unmanaged native entries. All expected handlers and support must match; any partial or changed entry returns no match for the whole selection. Unrelated handwritten settings and entries are neither matched as authority nor modified. Do not add OwnedIDs, persist a new record or grant cleanup authority.

Only then pass the non-owning boolean to existing legacy suppression planning. Reuse its safe surviving-marked-twin survey and byte-preservation behavior. An inspection or support safety error refuses the affected target before legacy mutation; a simple incomplete equivalence retains ordinary unproven output. Valid independent targets continue.

### D5: Descriptor-safe support verification

Inspect only declared native/support paths beneath configured repository roots. Reuse bounded no-follow descriptor reads, regular-file/link-count checks, existing aggregate/source byte limits and generation snapshots. Never open a path merely because a hook command names it. Symlinks into another HOME/cache are not portable proof and are not followed. Read-only checks leave all output/state generations, modes and modification times untouched, including absent state roots.

### D6: Subsequent writer policy is separately bounded

The native writer materializes repository-hook support as regular copies regardless of general symlink mode; other resource kinds and machine scope retain their existing policy. Conversion of an existing link requires current exact management/ownership evidence for its path and source, expected link generation and existing CAS/recovery publication. Unproven links are preserved/refused. Support is verified before native references; reconstruction itself grants no link removal or pruning authority.

This writer-policy change belongs to the subsequent native-writer delta, not the reader prerequisite. Read support ships first; no release or merge is performed automatically.

## Risks / Trade-offs

- [Projection drift] → Share the same HCC entry/serialization and materialize preparation seam with the writer; assert independently authored native outputs, not two calls to the same helper.
- [Stale source/IR] → Compile captured verified definition/support and reject mismatch; preserve raw/mirror domains and frozen vectors.
- [Private-proof bypass] → Existing selected-resource evidence/refusal takes precedence; test incomplete, drifted and corrupt records explicitly.
- [Support-path escape or private-cache dependence] → Verify declared regular copies only with descriptor safety and byte/mode equality; do not follow foreign links.
- [Cost on sync/check] → Native inspection is cached per target and scope. Verified source preparation currently runs per selected hook and does not share snapshots across resources in one definition. The retained disk-including public Run benchmark compares the immutable base and reader using one hook with 1 KiB of support. It does not establish multi-resource cost or cold-cache latency.
- [Legacy-twin deletion] → Feed complete satisfaction into existing twin-preserving suppression; never grant deletion authority from equivalence.
- [Prerequisite extraction scope] → Import hook/tool-only data and serializers; prove default legacy golden output and non-hook/machine behavior unchanged.

## Migration Plan

1. Add the shared native projection and independent reader with spec-bound, test-first public consumer witnesses. Keep default production writes legacy.
2. Run affected verification, frozen lock/fingerprint checks, strict specs, Full reader verification and fresh independent reviews; archive this prerequisite change as its final change commit and publish a separate review.
3. Human merges and releases the reader. Verify actual ancestry, release contents and asset hashes; do not invent a future version or digest.
4. Import the reviewed shared seam into the native writer and implement portable repository support/authorized link conversion with its own spec-bound red/green witnesses.
5. Pass the unchanged independent-HOME mixed-version gate against the actual release, complete affected Full gates and fresh reviews, then archive the existing native-writer change in its existing review before merge.

A rollback before writer activation simply retains legacy writes. Once a writer has migrated a repository, use only a reader release proven by the real compatibility gate; an older release is not assumed safe. Neither shared XDG state nor changed gate assertions is a rollout strategy.
