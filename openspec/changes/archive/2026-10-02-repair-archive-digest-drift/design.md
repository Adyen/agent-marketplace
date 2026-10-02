## Context

See proposal.md for the motivation and the production timeline.

Current mechanics that shape this design:

- Publication: `PublishRegistryRevision` uploads artifact blobs before the
  metadata transaction; `CommitRegistryPublication` then calls
  `putResourceRevision` per resource, which does `INSERT … ON CONFLICT
  (marketplace_id, registry_id, kind, name, content_digest) DO NOTHING`,
  reads the row back, and requires `archive_digest`, `license`, and
  `description` equality, returning `store.ErrConflict` otherwise. One
  conflicting resource rolls back the whole registry publication. The
  postgres and sqlite stores implement this symmetrically, and the registry
  occurrence upsert in the refresh path applies analogous checks.
- Consumer resolution: `getResourceRevision` prefers cached revision metadata
  (no TTL; non-rejected assessments stay usable indefinitely) and only then
  fetches, validating the fetched revision against the pin, including a hard
  `locked archive digest mismatch` error when `pin.LockArchiveDigest` no
  longer matches. Lock records carry per-resource archive digests.
- Update: `Update` skips a marketplace pin whose content digest is unchanged
  ("content digest unchanged") and performs no evidence refresh, so it cannot
  heal a drifted lock record.
- Worker: `managedWorkerLoop` logs `managed job did not complete` without a
  cause; `FailJob` records no cause. Managed previews already demonstrate the
  target observability pattern: a stable code plus a sanitized message, never
  raw git stderr.
- Blobs are immutable and content-addressed; no delete path exists, so a
  repaired artifact adds a new blob and leaves the old one resolvable.

## Goals / Non-Goals

**Goals:**

- Repair archive-digest drift in place during publication, keeping
  license/description immutability and old-artifact retention.
- Make locked consumers self-heal (plain `sync`, `update`) with a visible
  warning instead of a resolution error.
- Make failed managed refresh attempts carry their cause end to end (store,
  worker log, admin attempts surface).
- Production recovery without `serve reset`: the next scheduled refresh
  repairs awesome-ai's stored digests and republishes, preserving occurrence
  and publication history.

**Non-Goals:**

- No archive-format versioning in revision identity (considered; declined in
  favor of repair-in-place).
- No changes to artifact encoding or content digests; no new content
  revisions are manufactured.
- No backfill or migration job: the repair is lazy, on the next publication
  of each registry.
- Not addressing the separate, pre-existing intermittent refresh failures
  (roughly half of awesome-ai attempts from Sept 21–28, durations growing
  22s→122s); that needs its own follow-up.
- No blob garbage collection for superseded archive digests.

## Decisions

1. **Relax exactly the archive-digest dimension, inside the store.**
   `putResourceRevision` keeps the insert-and-read-back shape; on read-back
   mismatch it now updates `archive_digest` when the content digest matches
   (guaranteed by the conflict target) and only `archive_digest` differs,
   within the same publication transaction. License/description mismatches
   still conflict. Both stores change symmetrically. Alternative rejected:
   comparing in the ingestion layer, which would leave the occurrence upsert
   path enforcing the old invariant and split the rule across callers. The
   store owns the invariant, so the store relaxes it once.

2. **Consumer refresh is a warning plus a lock rewrite, not silent.**
   The resolver accepts the fetched archive digest for an identical content
   digest, updates the pin's lock evidence, and emits a stable diagnostic
   (working name `marketplace.archive-evidence-refreshed`), so users see the
   refresh. The lock writer then persists the fetched digest on the next
   writable reconciliation, matching plain `sync`'s minimal-reconciliation
   mandate. `Update`'s unchanged-content skip path records the fetched latest
   revision's archive digest into the lock evidence without a full
   re-prepare, so plain `update` heals drifted locks too. Alternative
   rejected: fixing only `update`; the failing path is cache-miss resolution
   during `sync`, which `update` never triggers.

3. **`sync --locked` stays strict, with an actionable message.** Locked mode
   forbids lock writes, so it cannot refresh evidence. On drift it fails with
   a message directing the user to plain `sync` or `update`, rather than
   proceeding with a lock/server divergence in a mode whose contract is
   byte-exactness. The auto-healing writable paths prevent stranding;
   `--locked` keeps its meaning.

4. **Failure causes follow the previews pattern.** `FailJob` gains a stable
   code plus a sanitized cause; the scheduled worker logs the cause with the
   failure line; the admin attempts surface and OpenAPI schema expose the
   fields (nullable on success). Alternative rejected: a separate error
   table; previews already proved the inline pattern and the admin surface
   reads one row per attempt.

5. **Repairs are logged per resource.** A publication logs registry,
   resource, and old→new archive digest when it repairs a row, so encoding
   drift is visible in worker logs without querying the store. This is
   operational visibility, not a spec requirement.

## Risks / Trade-offs

- [Weakened nondeterminism detection: a buggy encoder could flip archive
  digests silently] → the repair logs old→new digests per resource; artifact
  encoding stays deterministic and golden-tested; license/description
  immutability is retained as a second corruption signal.
- [Consumers on old CLI versions with cache loss still hit the mismatch
  error] → ship consumer and server repair in one release; the old error
  remains recoverable via remove+install; the exposed population is bounded
  (v2 lock records pinning affected skills).
- [`sync --locked` users see a hard error on drift] → intentional strictness
  with an actionable message; plain `sync`/`update` heals.
- [Blob accumulation: each encoding change adds one blob per affected
  resource] → immutable content-addressed storage; superseded-blob cleanup
  is a separate GC concern, out of scope.
- [The occurrence upsert path may enforce its own archive-digest checks] →
  a task audits the refresh-path upsert and applies the same relaxation
  wherever archive digests are pinned, so the reindex path cannot fail after
  the publication path was fixed.

## Migration Plan

1. Implement consumer refresh, store repair, and attempt-cause recording;
   ship as one release so consumers receive the self-heal with (or before)
   the catalog flip.
2. Deploy: the next scheduled awesome-ai refresh repairs the stored archive
   digests and republishes the registry, including `genai-psx`; no
   `serve reset`; occurrence and publication history preserved.
3. Rollback: reverting the server change re-freezes awesome-ai publications
   (today's state, no data loss); reverting the consumer change restores the
   mismatch error, and locks already refreshed remain valid records.

## Open Questions

- Final diagnostic code name for the consumer warning and the exact OpenAPI
  field names for the attempt cause (both deferrable naming choices with no
  structural effect).
