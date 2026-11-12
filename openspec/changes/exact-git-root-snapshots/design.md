## Context

See proposal.md for motivation. An exact snapshot is keyed by source and commit, checked out in full, and verified by hashing every tracked file. Every consumer reads a snapshot through a configured root: registry `path:`, a locked root's `path`, or an external plugin's subdirectory. Source revision creation already rejects symlinks inside the root that resolve outside it, so a consumer never needs files outside its root.

## Goals / Non-Goals

**Goals:**
- Acquire, verify, and hash only the configured root.
- Keep whole-repository snapshots from earlier releases usable.

**Non-Goals:**
- Cones derived from `include:` patterns; only `path:` narrows a snapshot.
- Any acquisition-lock, source revision, or diagnostic change.

## Decisions

**One snapshot per (source, commit, root).** A root is the cleaned slash path (`""` becomes `.`). Root `.` keeps the existing key, so existing whole-repository snapshots stay addressable. Any other root uses a key over source, commit, and root. Considered: one snapshot per lock record holding the union of its roots. Rejected because records are merged across layers by identity, so a union key would drift from what GC derives. Also considered: growing one shared snapshot in place. Rejected because republishing would remove a directory that a concurrent reader still holds.

**Root snapshots are used only when the root is a directory in the commit.** If `<commit>:<root>` is not a tree, for example because the path is missing or goes through a symlink, acquisition uses the whole-repository snapshot. The current behavior and errors are unchanged in that case.

**Root checkout leaves repository-local config inside the allowlist.** The snapshot has a detached `HEAD` at the commit, and its worktree holds exactly the tracked blobs under the root. No sparse-checkout configuration is persisted.

**Verification walks the consumed root only.** Expected entries come from the commit's tree limited to the root, using literal pathspecs. The walk starts at the root and requires every root path component to be a real directory, not a symlink. It rejects unexpected, missing, or mismatched entries under the root. It ignores everything outside the root. The per-command verification scope is keyed by snapshot path, commit, and root.

**Read-only opens fall back to the whole-repository snapshot.** A read-only open of a root first tries the root snapshot. If that snapshot is absent, the open uses the whole-repository snapshot of the same commit and verifies only the root inside it. Mutating acquisition always publishes or reuses the root snapshot.

**GC reachability adds root keys.** For every locked registry, the snapshot key of each locked root counts as reachable, together with the whole-repository key. The retention policy is unchanged.

## Risks / Trade-offs

- [A consumer reads outside its root] → It would find nothing in a root snapshot and fail loudly instead of reading unverified bytes. The command tests exercise every consumer.
- [Overlapping roots duplicate files on disk] → Accepted; snapshots stay immutable and independently verifiable.
- [Whole-repository snapshots linger while their commit is locked] → Same retention as today; they are collected once no lock pins the commit.

## Migration Plan

None required. Existing caches are read through the fallback, and mutating commands publish root snapshots as they run. Rolling back to an earlier release ignores root snapshots, and GC collects them.
