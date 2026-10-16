## Context

File-mode copy publication already distinguishes three cases for an existing destination: not recorded in the materialization manifest (unmanaged), recorded with a digest that matches what sync last wrote (replace or adopt), and recorded with a digest that no longer matches (preserve). The last two preserved cases currently share the unmanaged-collision message. The return-to-symlinks path makes the same recorded-and-changed decision before refusing to replace the copy with a link.

## Goals / Non-Goals

**Goals:** give the recorded-and-changed case its own message at every place that makes that decision, while leaving the decision itself alone.

**Non-Goals:** digestless legacy manifest entries (ownership cannot be told apart from an upstream change, so they keep the unmanaged message), the prune-time "preserved modified materialized output" warning, native marker and shared-configuration collisions, and symlink-mode edits that land in the cache.

## Decisions

- Keep the code `materialize.collision.user-owned` and warning severity. Scripts and diagnostic grouping match on the code; only the message is wrong. A new code was considered and rejected to avoid breaking those consumers.
- Build the message in one shared helper used by the skill-tree, single-file, and return-to-symlinks paths, so the wording cannot drift between them. Suggested wording: `local edit: preserved locally edited copy "<path>" of resource "<name>" (managed by agent-marketplace; its contents changed since the last sync, so registry updates are not applied); to restore the registry version, delete or move it and run agent-marketplace sync`.
- Select the message only when the manifest entry exists and carries a digest; the existing ownership decision already returns the preserve verdict for that case, so no new filesystem reads are needed.

## Risks / Trade-offs

- [Tests or tooling that match the old message text for edited copies] → only tests in this repository match message text; they are updated in the same change.
