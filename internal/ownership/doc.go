// Package ownership is the durable, crash-recoverable authority for which
// entries of a SHARED harness aggregate file (Devin's hooks.v1.json,
// config.json#hooks, and mcp_config.json) were written by Agent Marketplace.
//
// It implements work package D9 of
// docs/plans/2026-08-03-devin-first-class-harness.md. Every rule below is
// quoted from that plan; nothing here is invented.
//
// # Two stores, two failure modes
//
// "Ownership authority is stored outside repositories and outside caches"
// (plan, "Durable state"). There are therefore exactly two on-disk stores that
// matter, and they behave differently on purpose:
//
//   - $XDG_STATE_HOME/agent-marketplace (default ~/.local/state/agent-marketplace)
//     is AUTHORITATIVE. "Missing or corrupt ownership state makes all existing
//     aggregate entries unmanaged" — sync then preserves everything, refuses
//     collisions, and prunes nothing. That is the fail-closed direction: the
//     worst outcome is a stale entry a user can delete, never a deleted entry a
//     user hand-wrote.
//   - ~/.cache/agent-marketplace is a CACHE. "Cache deletion has no ownership
//     effect." Nothing in this package reads it, so deleting it cannot change a
//     single ownership answer. TestCacheDeletionIsHarmless pins that.
//
// # Absent state is not the same fact as vanished state
//
// Both fail closed identically, but only one is worth telling a user about. A
// first-ever run has no record because nothing was ever recorded; that is
// normal and is reported as [ErrStateUninitialized] (a wrapper of
// [ErrStateMissing]), which callers log at debug rather than warn. A root that
// [Store.EverRecorded] — it has an ownership directory or a retained safety
// backup — but whose record is gone is reported as plain [ErrStateMissing] and
// stays a user-facing ownership.state-missing warning. The classification comes
// from durable evidence this package wrote, never from aggregate bytes, and it
// changes only the REPORT: [Proof] is the zero value, and [Proof.Classify]
// answers StatusUnmanaged, in both cases.
//
// The evidence is keyed by the canonical root, so anything that destroys it or
// changes the root's identity (a moved checkout, a relocated XDG_STATE_HOME, a
// case-different spelling) reads as a first use and reports NOTHING. That is a
// known residual, spelled out on [ErrStateUninitialized]; it never affects what
// is classified.
//
// # Ownership is proven, never inferred
//
// "Without proof of ownership, sync preserves existing entries and refuses
// collisions or pruning" and "ownership is never inferred heuristically".
// This package makes heuristic inference STRUCTURALLY impossible rather than
// merely discouraged:
//
//   - The only type that answers "is this entry mine?" is [Proof]. Its fields
//     are unexported, it has no literal constructor outside this package, and
//     the only way to obtain a non-zero one is [Store.Proof], which reads the
//     durable record.
//   - [Proof.Classify] requires an [EntryID] plus the entry's CURRENT value
//     digest, and compares both against digests this package previously
//     recorded. There is no exported function anywhere that takes only the
//     current file bytes (or a marker field inside them) and returns an
//     ownership verdict.
//   - Orphan nomination requires an [Evidence] value, whose fields are also
//     unexported and which is only ever produced from a retained safety-backup
//     manifest or journal. [OrphanCandidate] has no "owned" accessor at all, so
//     a caller cannot accidentally describe a candidate as currently owned.
//   - [ConflictCandidate] — an unmanaged entry whose key matches a desired entry
//     but whose value differs — is likewise only obtainable from
//     [ValueConflictCandidates], which requires the unmanaged verdict plus
//     digest INEQUALITY. It carries two digests and no value, and it is never
//     described as owned either.
//
// # Three repair classes, two safety contracts
//
// [ExactAdoptionCandidates], [Store.OrphanCandidates], and
// [ValueConflictCandidates] are disjoint by construction, and they split into
// two safety contracts along ONE line: does acting destroy bytes the user wrote?
//
//	adoption          exact value match          byte-neutral   batchable with --yes
//	orphan removal    evidence-backed, undesired DESTRUCTIVE    per-entry confirmation + fresh backup
//	value conflict    same key, different value  DESTRUCTIVE    per-entry confirmation + fresh backup
//
// The two destructive classes share [Mutation.Confirmed] and therefore share one
// enforcement point, [validateMutation]; see [ReasonConflictReplace].
//
// # What D10 and D11 call
//
// D10 (Devin materialization) calls, per canonical root, with the WHOLE
// read-modify-commit inside ONE write session:
//
//	st, err := ownership.Open(targetRoot)
//	err = st.WithWriteSessionContext(ctx, "reconcile", func() error {
//	    outcomes, err := st.Recover()         // once, before any mutation
//	    for _, rel := range aggregateFiles {  // sequential mutations, one section
//	        raw, readErr := os.ReadFile(abs)  // the READ must be inside: see below
//	        proof, err := st.Proof(rel)       // err is advisory; proof is fail-closed
//	        ... classify with proof.Classify, patch with pkg/harness-compilation/jsonc ...
//	        gen, priorDigest := proof.Generation(), ""
//	        if readErr == nil { priorDigest = ownership.Digest(raw) }
//	        res, err := st.Apply(ownership.Mutation{
//	            Rel: rel, AbsFile: abs, NextBytes: patched, Entries: owned, Reason: ownership.ReasonSync,
//	            ExpectPriorGeneration: gen, ExpectPriorDigest: priorDigest,
//	        })
//	    }
//	    return nil
//	})
//
// The session is not optional and the read is why. [Store.Apply] takes the per-root
// lock itself, but it cannot serialize a read its CALLER already performed: two
// processes that each read the aggregate and then commit produce a second patch
// computed from bytes the first one already replaced, which silently drops the first
// one's entry. Wrapping the read makes the whole sequence atomic across processes;
// the inner Recover and Apply acquisitions reenter the held lock at a bumped depth,
// and being SEQUENTIAL they do not trip [ErrNestedMutation].
//
// Use [Store.WithWriteSession] (or its Context form), never [Store.WithLock], for
// this: WithLock returns the acquisition error and does not run the body, so on a
// filesystem with no advisory locking it would turn "this root is not serialized"
// into "this root is never reconciled".
//
// # Serialization
//
// Ownership mutations are serialized per canonical target root, not per file
// and not per process. The serialization primitive is an advisory flock(2) on
// $XDG_STATE_HOME/agent-marketplace/locks/<rootKey>.lock. The lock belongs to
// the open file description, so the kernel releases it on Close, exit, panic,
// SIGKILL, OOM-kill, and container teardown. There is no stale-lock policy,
// no PID liveness check, no timestamp threshold, and no break/steal protocol,
// and the lock file is never unlinked.
//
// Because the lock is per root, one session covers Recover and every aggregate
// file of that root. Callers that read an aggregate, derive a patch, and then
// commit with Store.Apply must wrap the whole read-modify-commit in
// Store.WithWriteSession (or WithWriteSessionContext); Apply's own lock cannot
// serialize a read the caller performed before calling it.
//
// Acquisition polls with a jittered backoff instead of blocking, so a cancelled
// or timed-out wait does not leak a file descriptor that could later acquire
// the lock and manufacture a stale lock. AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT
// overrides the default; 0 means fail fast, and malformed or negative values are
// refused by Open.
//
// On filesystems that do not support advisory locking, Apply degrades to the
// unserialized path and reports ownership.lock-unavailable. The compare-and-swap
// precondition in Apply (Mutation.ExpectPriorGeneration and
// Mutation.ExpectPriorDigest) is the portable backstop: if another writer or an
// external tool changed the durable record or aggregate after the caller read it,
// Apply returns ownership.generation-conflict and leaves the file unchanged.
//
// Reads (Load, Proof, ReadJournal, SafetyBackups, OrphanCandidates) take no
// lock. Aggregate files are replaced by rename, and the durable record is
// written only after the aggregate is published, so a lock-free reader can see
// a record one generation behind the file. That is safe: the worst case is an
// entry classified as Unmanaged or OwnedDrifted, both of which preserve user
// bytes.
//
// A DRY RUN takes no session and no lock at all: it performs no mutation, and
// acquiring would create locks/ under the state root, which [Open]'s side-effect-free
// contract forbids. Bead agent-marketplace-wrp8 owns this recipe; the full
// serialization contract is prose owned by B6
// (docs/plans/2026-08-06-ownership-locking-design.md is the authority until then).
//
// D11 (doctor/repair) calls [ExactAdoptionCandidates],
// [Store.OrphanCandidates], and [ValueConflictCandidates] for the preview, then
// [Store.Apply] with [ReasonAdoption], [ReasonOrphanRemoval], or
// [ReasonConflictReplace]; the two destructive reasons are refused unless
// Mutation.Confirmed is set, so an off-TTY preview cannot mutate anything.
package ownership
