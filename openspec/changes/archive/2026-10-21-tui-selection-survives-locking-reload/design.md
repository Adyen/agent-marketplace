## Context

`rowKey` identifies a row by kind, name, catalog key, source URL, ref, path, artifact path and malformed state. The catalog key is the mutable cache key (`URL+ref+path` hashed into the cache directory) when the source has no lock, and the commit-qualified snapshot identity once it has one. Rebuild anchoring (`captureRebuildAnchor` / `restoreSelectionAfterRebuild`) and the open-detail rebuild (`rebuildOpenBundle`) match only that key, apart from a narrower fallback for unavailable and pending rows.

## Goals / Non-Goals

**Goals:** keep the selected resource, and an open detail view, through reloads that change only the catalog that loads it.

**Non-Goals:** changing `rowKey`, which also keys detail requests, toggle forms, Marketplace inspections and ordering ties; Marketplace revision changes, whose source ref carries the revision.

## Decisions

- **Fallback, not a new key.** `rowKey` keeps distinguishing rows whose catalog differs, for example local sources with the same relative path resolved against different roots. The exact key is tried first, then a resource identity built from kind, name, the existing `registryIdentityKey` (normalized URL, ref, path) and the malformed flag. This is the same notion of source identity the unavailable-row fallback already uses.
- **Unique match only.** The fallback selects a row only when exactly one row has the identity. Otherwise the existing position-based behavior stays, so the change cannot select a different resource than before.
- **Prove it in the real flow.** The journey that exposed the race now waits for the settled state. On the old code it fails deterministically, because the selection lands on `beta-skill`.

## Risks / Trade-offs

- [Identity collision across catalogs] → handled by the uniqueness rule; nothing is guessed.
