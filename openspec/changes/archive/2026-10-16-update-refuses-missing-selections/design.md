## Context

`update` publishes acquisition locks inside the lock lifecycle (`prepareRuntimeLocks` with upgrade mode) and only afterwards runs the sync that resolves selections. Non-required misses become warnings and required misses fail the sync after the lock has advanced. Evidence: day-2 experiment transcripts S1 to S6.

## Goals / Non-Goals

**Goals:** refuse before publication when a selection newly stops resolving; leave every byte unchanged on refusal; keep already-stale selections as warnings.

**Non-Goals:** Marketplace pin 404 handling, organization relock, sync/status/prune-stale behavior, automatic pruning, collection selector edges, docs.

## Decisions

- **Pre-publication guard in the lock lifecycle.** An optional selection guard on the lock mode runs after the new exact snapshots are acquired and verified and before the dependency transaction commits. It runs only for upgrade mode when at least one acquisition lock would change, so no-op updates pay nothing. Only `update` installs it.
- **Two resolutions, one diff.** The guard resolves the effective configuration twice without recording inventory: once against the prior lock documents (on-disk configuration, no staged overlays) and once against the desired documents (staged config and lock overlays applied). Exact-snapshot catalog lookups for both reuse the same lookup construction as the final catalog option. A selection is newly missing when its not-found diagnostic appears in the new resolution and not in the prior one. The identity of a diagnostic is kind, name, canonical source URL, source path, and the collection name it came through (without the collection version); the ref is excluded so `update --bump` compares like with like.
- **Unknown prior state is conservative.** When the prior revision cannot be evaluated (no prior lock document for that layer, or its snapshot cannot be acquired), every miss at the new revision counts as newly missing, because no evidence shows it was already stale.
- **Typed refusal surfaced directly.** The guard returns a typed refusal listing the misses (deduplicated by the identity above, in stable order). `update` returns it unwrapped, so the output is the refusal header, one line per miss using the existing `SelectedNotFoundError` text, and the fixed hint. No journal is opened because the guard runs before commit.
- **Other diagnostics untouched.** Non-not-found resolution errors in the guard do not refuse; the final sync reports them as today. Directory and unlocked organization sources resolve identically in both passes, so they never produce a diff.

## Risks / Trade-offs

- Two extra resolutions when a lock changes. Accepted: they reuse already-acquired snapshots and cached catalogs.
- Prior snapshot acquisition can fetch an old commit online. If it fails, the conservative rule applies and the hint tells the user how to proceed.
