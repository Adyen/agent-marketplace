package ownership

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// This file is the portable half of the per-canonical-root advisory lock
// designed in docs/plans/2026-08-06-ownership-locking-design.md. The platform
// half is filelock_unix.go (real flock(2)) and filelock_other.go (a stub that
// reports [ErrLockUnsupported]).
//
// # Why there is no stale-lock policy, and why nobody may add one
//
// A flock(2) lock is owned by the OPEN FILE DESCRIPTION, not by a process, a
// PID, or a path. The kernel therefore drops it on Close, on normal exit, on
// panic, on SIGKILL, on OOM-kill, and on container teardown. There is no such
// thing as a stale flock, so every mitigation a lock-file design would need is
// not merely unnecessary here, it is actively wrong:
//
//   - No PID liveness check. A PID is meaningless across a PID namespace and is
//     reused, so "the holder is gone" cannot be decided from one.
//   - No timestamp threshold. It is wrong under clock skew and wrong for a
//     legitimately long critical section, and both errors break mutual
//     exclusion rather than degrading it.
//   - No break/steal protocol. Breaking is itself racy: two processes can decide
//     to break the same lock and then both proceed.
//   - NEVER unlink the lock file. Unlink-and-recreate splits the lock across two
//     inodes: A holds a lock on the old inode, B creates a new one, C locks the
//     new inode, and A and C both believe they hold "the" lock. The lock file is
//     created once per canonical root and left in place forever. One small file
//     per root ever used is bounded, tiny, and is deliberately NOT garbage
//     collected by `gc`.
//
// The lock file's contents (PID, hostname, argv[0], acquisition timestamp) are
// written after the lock is held and are diagnostics only. Nothing reads them to
// decide anything, so nothing can be fooled by them.
//
// # Why acquisition polls instead of blocking
//
// The retry loop uses a non-blocking flock plus jittered backoff rather than a
// blocking flock on a goroutine. A blocking flock cannot be cancelled: past the
// deadline its goroutine and its file descriptor would leak, and a leaked
// descriptor that later ACQUIRES the lock nobody is waiting on is the single
// way to manufacture a stale lock in a mechanism that otherwise has none. Do not
// "improve" this into a blocking call.

const (
	// lockDirName holds one lock file per canonical target root. It is a SIBLING
	// of ownershipDirName and safetyDirName, never a child of either:
	// [Store.EverRecorded] reads the existence of the ownership directory and of
	// the safety-backup directory as durable evidence that this root was managed,
	// so a lock file created under one of them would turn every first-ever run's
	// silent ErrStateUninitialized into a user-facing ownership.state-missing
	// warning.
	lockDirName = "locks"
	// lockFileExt is appended to the root key to name the lock file.
	lockFileExt = ".lock"

	// lockBackoffMin and lockBackoffMax bound the retry interval used while
	// waiting for a contended lock.
	lockBackoffMin = 25 * time.Millisecond
	lockBackoffMax = 200 * time.Millisecond
	// lockBackoffJitter is the fraction of the interval randomized in each
	// direction, so N waiters do not resynchronize into a thundering herd.
	lockBackoffJitter = 0.2
)

// LockContendedReportThreshold is how long a wait must last before it is worth
// telling a user about (diag.CodeOwnershipLockContended). A few milliseconds of
// contention is the mechanism working as designed, and reporting it would make
// every save-hook-versus-human race produce a line nobody can act on.
//
// It is EXPORTED because the same threshold has to gate both halves of one
// report: the slog.Info in acquireLock, which is the only place a successful wait
// is visible in the log, and the diag.CodeOwnershipLockContended diagnostic a
// caller derives from a LockAcquired event's Waited field (internal/materialize's
// Devin reconciler). Two independent copies of this number would let a user see a
// diagnostic with no corresponding log line, or the reverse, so there is one.
const LockContendedReportThreshold = time.Second

// DefaultLockTimeout bounds how long acquisition waits for another process to
// finish its critical section.
//
// The section is milliseconds long (a handful of reads, a jsonc patch, and the
// fsyncs of one journaled generation); git fetch, catalog load, resolution, and
// harness compilation all happen outside it. 30s is therefore very much larger
// than any legitimate section and very much smaller than any CI job timeout, so
// an expiry is always a real anomaly worth reporting rather than a tuning knob.
//
// AGENTS.md's defaults policy puts overridable defaults in internal/config, and
// config.DefaultOwnershipLockTimeout is that documented home. This package
// deliberately does NOT import it, and the duplication is pinned by
// TestDefaultLockTimeoutMatchesTheConfigDefault rather than left to trust.
//
// The reason is LAYERING, and it is worth stating accurately because the obvious
// stronger claim is false: importing internal/config here compiles fine, for
// GOOS=windows as much as for unix, so TestNonUnixBuildCompiles would keep
// passing. What the import would actually cost is this package's independence.
// internal/ownership is the durable-state leaf: its only non-stdlib dependency is
// pkg/harness-compilation/diag, and the import would give the ownership record,
// the journal, the mandatory safety backups and this lock a transitive dependency
// on the YAML config loader (gopkg.in/yaml.v3, internal/skill, ir) purely to read
// one integer. A test-only import keeps both properties — the policy's home for
// the value and a dependency-free writer — because `go build
// ./internal/ownership/...` ignores test files, and any drift between the two
// declarations is a failing test rather than a silent divergence.
const DefaultLockTimeout = 30 * time.Second

// LockTimeoutEnvVar overrides [DefaultLockTimeout] with a Go duration.
//
// It is the escape hatch diag.CodeOwnershipLockTimeout's hint promises users
// ("wait for that run to finish, or raise
// AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT"), so it has to exist wherever that
// hint can be printed: a hint naming a knob that is implemented nowhere is worse
// than no hint at all.
//
// Precedence is ENV VAR > in-process configuration ([WithLockTimeout]) > the code
// default, per AGENTS.md. The env var deliberately wins over the option because
// the option is how this repository's callers and tests pass a value, while the
// env var is how a human overrides a decision the program already made — a CI
// runner that legitimately queues two syncs cannot edit the call site.
//
// `0` means fail fast (exactly one non-blocking attempt). A value that does not
// parse, or is negative, is REJECTED by [Open] rather than silently ignored:
// silently falling back would leave a user who mistyped the knob believing their
// override is in force, and the failure is loud, immediate, and identical on
// every command.
const LockTimeoutEnvVar = "AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT"

// ErrLockTimeoutConfig reports an unusable [LockTimeoutEnvVar] value.
var ErrLockTimeoutConfig = errors.New("ownership: invalid " + LockTimeoutEnvVar)

// lockTimeoutFromEnv resolves [LockTimeoutEnvVar]. An unset or all-whitespace
// value is "not configured", not zero: zero means fail fast and must be asked
// for explicitly.
func lockTimeoutFromEnv() (time.Duration, bool, error) {
	raw, ok := os.LookupEnv(LockTimeoutEnvVar)
	if !ok {
		return 0, false, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%w: %q is not a Go duration (for example 30s, 500ms, or 0 to fail fast)", ErrLockTimeoutConfig, raw)
	}
	if d < 0 {
		return 0, false, fmt.Errorf("%w: %q is negative; use 0 to fail fast", ErrLockTimeoutConfig, raw)
	}
	return d, true, nil
}

var (
	// ErrLockUnsupported reports that advisory locking is unavailable: either the
	// platform has no flock(2) (see filelock_other.go) or the filesystem refuses
	// it (ENOTSUP/EOPNOTSUPP, realistically an NFS-mounted state root).
	//
	// ENOLCK is treated as transient lock-table exhaustion and is retried like
	// contention; it only degrades here if it persists to the lock timeout.
	//
	// It is NOT a failure to be swallowed silently: a caller degrades to exactly
	// today's unserialized behaviour and reports
	// diag.CodeOwnershipLockUnavailable.
	ErrLockUnsupported = errors.New("ownership: advisory file locking is not supported here")

	// ErrLockTimeout reports that another holder still had the lock when the
	// timeout expired. The caller must leave the root exactly as found; it is
	// deliberately distinguishable from every other acquisition failure so a
	// caller can report diag.CodeOwnershipLockTimeout rather than a generic
	// error.
	ErrLockTimeout = errors.New("ownership: timed out waiting for the ownership lock")

	// errLockBusy is the portable spelling of EWOULDBLOCK. It never escapes this
	// package: the retry loop consumes it and reports ErrLockTimeout instead.
	errLockBusy = errors.New("ownership: lock is held by another process")
)

// LockEventKind names one observable transition of the per-root lock.
type LockEventKind string

const (
	// LockAcquired: this Store took the lock with its own file descriptor.
	LockAcquired LockEventKind = "acquired"
	// LockReentered: a nested acquisition on a Store that already holds the lock
	// bumped the depth counter instead of opening a second descriptor. A second
	// descriptor would contend with the first — flock is per open file
	// description, not per process — and would therefore self-deadlock.
	LockReentered LockEventKind = "reentered"
	// LockContended: the first non-blocking attempt failed and the acquisition
	// had to wait.
	LockContended LockEventKind = "contended"
	// LockTimedOut: the deadline expired while another holder had the lock.
	LockTimedOut LockEventKind = "timed-out"
	// LockCanceled: the caller's context was cancelled while waiting. It is
	// deliberately distinct from LockTimedOut: nothing expired, no other process
	// is at fault, and there is nothing to report to a user who pressed Ctrl-C.
	LockCanceled LockEventKind = "canceled"
	// LockUnsupported: this platform or filesystem refuses advisory locks.
	LockUnsupported LockEventKind = "unsupported"
	// LockCASConflict means Apply's compare-and-swap precondition failed.
	LockCASConflict LockEventKind = "cas-conflict"
	// LockReleased: one depth level is being released. At depth zero the observer
	// runs immediately before the descriptor is unlocked and closed, so it can
	// bracket the exact kernel-held section.
	LockReleased LockEventKind = "released"
)

// LockEvent describes one transition for [WithLockObserver].
//
// The observer exists for three reasons: so a later compare-and-swap bead can
// assert that its precondition never fires on a locked path, so a test can count
// contention deterministically instead of sleeping and hoping, and — since the
// section was widened over internal/materialize's read — so the reconciler can
// turn a WAIT into a per-root report diagnostic. A wait that succeeds is not an
// error, so it reaches the report through no error value; the observer is the only
// place it is visible. It carries no aggregate content and no entry value — only
// the root key, the lock path, timings, and counters.
type LockEvent struct {
	Kind     LockEventKind
	RootKey  string
	Path     string
	Depth    int
	Waited   time.Duration
	Attempts int
	Err      error
}

// LockObserver is notified of every lock transition. It must not block and must
// be safe for concurrent use: two Stores for one root can report from two
// goroutines. A LockReleased callback runs while this Store's acquisition mutex
// is held; at depth zero, the kernel lock is also still held. It must not call
// Lock or a mutating method on that Store.
type LockObserver func(LockEvent)

// WithLockTimeout bounds how long [Store.Lock] waits for a contended lock.
//
// Zero means fail fast: exactly one non-blocking attempt, then [ErrLockTimeout].
// A negative duration is clamped to zero rather than rejected, so a misconfigured
// value can never mean "wait forever".
//
// [LockTimeoutEnvVar] OVERRIDES this option, because a user's escape hatch has to
// beat a decision compiled into a call site. Precedence is env var > this option >
// [DefaultLockTimeout].
func WithLockTimeout(d time.Duration) Option {
	return func(s *Store) {
		if d < 0 {
			d = 0
		}
		s.lockTimeout = d
	}
}

// WithLockObserver installs a [LockObserver].
//
// internal/materialize's Devin reconciler installs one in production, to report
// contention and degradation per root; everything else that installs one is a
// test. An observer must therefore stay cheap and must never mutate anything.
func WithLockObserver(obs LockObserver) Option {
	return func(s *Store) { s.lockObserver = obs }
}

// lockSession is one held lock: the descriptor whose open file description owns
// the kernel lock, and the reentrancy depth of this Store.
type lockSession struct {
	f     *os.File
	depth int
}

// LockPath returns the advisory lock file for this canonical root:
// <state root>/locks/<rootKey>.lock.
//
// It is a sibling of the ownership record directory and of the safety-backup
// directory, never a child of either, so it is not evidence of anything. See
// [Store.EverRecorded].
func (s *Store) LockPath() string {
	return filepath.Join(s.stateRoot, lockDirName, s.rootKey+lockFileExt)
}

// LockDepth reports how many nested acquisitions this Store currently holds.
// Zero means this Store does not hold the lock. It exists so a test, and the
// later CAS bead, can assert the reentrancy contract without reaching into
// unexported state.
func (s *Store) LockDepth() int {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if s.session == nil {
		return 0
	}
	return s.session.depth
}

// HoldsLock reports whether this Store currently holds the per-root lock.
func (s *Store) HoldsLock() bool { return s.LockDepth() > 0 }

// Lock acquires the per-canonical-root advisory lock and returns a release
// function. The release function is idempotent: calling it twice releases one
// depth level, not two, so `defer release()` is always safe.
//
// It is REENTRANT PER STORE. A nested acquisition on a Store that already holds
// the lock bumps a depth counter and reuses the existing descriptor. Opening a
// second descriptor instead would deadlock against the first, because flock
// associates a lock with the open file description rather than with the process:
// two descriptors on one path contend even inside one process.
//
// Reentrancy is per Store and NOT per process. Two Stores for one root — two
// goroutines, two commands, two processes — hold two descriptions and therefore
// genuinely serialize against each other. That is what makes the acceptance
// tests a test of the mechanism rather than of an in-process mutex.
//
// The corollary is the one thing a caller can get catastrophically wrong: ONE
// STORE SHARED BY TWO GOROUTINES IS NOT MUTUAL EXCLUSION BETWEEN THEM. The
// second acquisition takes the reentrant path, gets a releaser, and runs its
// section at the same time as the first — exactly as if there were no lock at
// all, while every other process stays correctly excluded. A caller that wants
// two concurrent sections serialized must give each one its own Store (or its
// own in-process mutex). Concurrent acquisition on one Store is safe — it is
// serialized here, and the second caller reenters rather than self-contending —
// but safe is not the same as exclusive.
//
// Failure modes a caller must distinguish:
//
//   - [ErrLockTimeout]: another holder still had it at the deadline. Leave the
//     root exactly as found and report diag.CodeOwnershipLockTimeout.
//   - [ErrLockUnsupported]: no advisory locking here. Degrade to today's
//     unserialized behaviour and report diag.CodeOwnershipLockUnavailable.
//   - anything else (permission denied on the state root, ENOSPC): a hard
//     failure, and the same conditions under which the mandatory safety backup
//     would fail anyway.
//
// Lock is not a side effect of [Open]: Open is contractually side-effect free so
// that read-only commands (status, list, sync --check, the TUI refresh) touch
// nothing. Lock is the one call that creates locks/.
func (s *Store) Lock() (release func(), err error) {
	return s.LockContext(context.Background())
}

// LockContext is [Store.Lock] with cancellation.
//
// The backoff loop checks ctx.Done alongside the remaining time, so a contended
// acquisition abandons the wait the moment its caller is cancelled instead of
// sitting out the whole timeout. Without it a `sync` that hit contention ignored
// Ctrl-C for up to [DefaultLockTimeout] — 30 s of a terminal that will not quit —
// even though every command already has a context.
//
// A cancelled wait is NOT [ErrLockTimeout]: no timeout expired and there is
// nothing to report to the user about another process, so it returns ctx.Err()
// wrapped. Like every other acquisition failure, the releaser is nil.
func (s *Store) LockContext(ctx context.Context) (release func(), err error) {
	// acquireMu spans the WHOLE acquisition, not just the session check. Without
	// it two goroutines sharing one Store can both observe session == nil, both
	// open a descriptor, and — because flock is per open file description —
	// contend with EACH OTHER: the loser waits out the entire timeout and then
	// reports ownership.lock-timeout, blaming another process for a lock this
	// Store itself holds. With it, the second caller waits for the first
	// acquisition to finish and then takes the reentrant path it was promised.
	s.acquireMu.Lock()
	defer s.acquireMu.Unlock()

	s.lockMu.Lock()
	if s.session != nil {
		s.session.depth++
		depth := s.session.depth
		s.lockMu.Unlock()
		s.observeLock(LockEvent{Kind: LockReentered, Depth: depth})
		return s.releaser(), nil
	}
	s.lockMu.Unlock()

	f, waited, attempts, err := s.acquireLock(ctx)
	if err != nil {
		return nil, err
	}

	anchorHeldDescriptor(f)
	s.lockMu.Lock()
	s.session = &lockSession{f: f, depth: 1}
	s.lockMu.Unlock()

	s.writeLockMetadata(f)
	s.observeLock(LockEvent{Kind: LockAcquired, Depth: 1, Waited: waited, Attempts: attempts})
	return s.releaser(), nil
}

// WithLock runs fn with the per-root lock held, releasing it however fn returns.
func (s *Store) WithLock(fn func() error) error {
	return s.WithLockContext(context.Background(), fn)
}

// WithLockContext is [Store.WithLock] with cancellation. See [Store.LockContext].
func (s *Store) WithLockContext(ctx context.Context, fn func() error) error {
	release, err := s.LockContext(ctx)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// WithWriteSession runs a CALLER's whole read-modify-commit with the per-root
// lock held, under exactly the policy every write entry point in this package
// uses. op is a short verb for the diagnostic (for example "reconcile").
//
// It exists because [Store.WithLock] is the wrong primitive for widening a
// section, and the difference is not cosmetic. WithLock is the raw mechanism: it
// returns the acquisition error and DOES NOT RUN the body. So a caller that
// widened its section with WithLock would, on a filesystem with no advisory
// locking, skip the reconciliation entirely and report an error — turning "this
// root is not serialized" into "this root is never reconciled", on precisely the
// platforms the design commits to degrading on rather than failing (GOOS=windows
// unconditionally, and an NFS-mounted state root). WithWriteSession degrades and
// warns instead, identically to [Store.Apply].
//
// This is the seam the section-widening bead (B3) must use around the aggregate
// READ, which is the one thing Apply's own acquisition cannot cover: Apply cannot
// serialize a read its caller performed before calling it. Inside the session,
// Apply, Recover, and BackupOnly reenter the held lock at a bumped depth rather
// than opening a second descriptor.
//
// It is per STORE, like every other acquisition here, so it is not mutual
// exclusion between two goroutines sharing one Store. See [Store.Lock].
func (s *Store) WithWriteSession(op string, fn func() error) error {
	return s.withWriteLock(op, fn)
}

// WithWriteSessionContext is [Store.WithWriteSession] with cancellation, and it
// is what internal/materialize's reconciler uses: `materialize.Run` already has a
// context, so a sync that hits contention must answer Ctrl-C rather than sit out
// the timeout. See [Store.LockContext].
func (s *Store) WithWriteSessionContext(ctx context.Context, op string, fn func() error) error {
	return s.withWriteLockContext(ctx, op, fn)
}

// ErrNestedMutation reports that a mutation was started on a Store that is
// already inside one.
//
// It is a separate guard from the lock, because it is the one hazard the lock
// STRUCTURALLY cannot cover: acquisition is reentrant per Store, so a second
// mutation on the same Store gets a depth bump rather than exclusion. Two
// SEQUENTIAL mutations inside one session (Recover, then N × Apply) are the
// pattern reentrancy exists for and are unaffected; what this refuses is a second
// mutation starting while the first one's generation is half written.
//
// Unserialized that interleaving corrupts state THROUGH the lock: the journal is
// one slot per canonical root and Record.Generation is one counter for all of the
// root's files, so the inner generation writes and then CLEARS the journal the
// outer one depends on, and the outer commitRecord — built from a Load that
// predates the inner commit — silently drops the inner generation's file from
// Record.Files.
//
// It also refuses two CONCURRENT mutations on one shared Store, which is the same
// condition seen from a different angle and which the lock likewise cannot
// exclude (see [Store.Lock]: one Store shared by two goroutines is not mutual
// exclusion). Refusing is the fail-closed answer and no caller in this repository
// does either thing: give each concurrent writer its own Store.
var ErrNestedMutation = errors.New("ownership: a mutation cannot start while another one is in flight on this store")

// withMutation runs ONE mutation body — [Store.Apply], [Store.Recover], or
// [Store.BackupOnly] — inside the per-root section, refusing to start while this
// Store already has a mutation in flight.
//
// The guard is deliberately INSIDE withWriteLock rather than around it, so that
// [Store.WithWriteSession] can widen the section over a caller's read and still
// contain any number of sequential mutations.
func (s *Store) withMutation(op string, fn func() error) error {
	return s.withWriteLock(op, func() error {
		s.mutatingMu.Lock()
		if s.mutating {
			s.mutatingMu.Unlock()
			return fmt.Errorf("%w: %s", ErrNestedMutation, op)
		}
		s.mutating = true
		s.mutatingMu.Unlock()
		defer func() {
			s.mutatingMu.Lock()
			s.mutating = false
			s.mutatingMu.Unlock()
		}()
		return fn()
	})
}

// withWriteLock runs the body of ONE WRITE entry point with the per-root lock
// held. [Store.Apply], [Store.Recover], and [Store.BackupOnly] are its three
// in-package callers and they are the whole set of mutating entry points this
// package has; [Store.WithWriteSession] exposes the same policy to a caller that
// needs to widen the section over its own read.
//
// # Why every writer, and no reader
//
// A writer ACTS on what it read: Apply's generation number and next record come
// from a Load that happened microseconds earlier, and Recover decides between
// roll-forward, roll-back, and abandon from a journal it read the same way. Two
// unserialized writers therefore publish the aggregate and the record at
// different instants. A reader only REPORTS what it read, and the worst view it
// can obtain — a record one generation behind the file, since both are only ever
// replaced by rename — classifies as Unmanaged or OwnedDrifted, which are
// preserve-don't-prune outcomes. So no read path may acquire this lock: doing so
// would also create locks/<rootKey>.lock on `status` and `sync --check`, which
// [Open]'s side-effect-free contract forbids, and would make a read-only command
// block behind a long-running sync.
//
// # Reentrancy, and the one way to misuse it
//
// Acquisition here is reentrant per Store, which is what lets a caller widen its
// section around Recover plus N × Apply without the inner acquisitions
// deadlocking. Reentrancy is per STORE and not per process, so ONE STORE SHARED
// BY TWO GOROUTINES IS NOT MUTUAL EXCLUSION BETWEEN THEM: the second acquisition
// is a depth bump and both bodies run at once, while every other process stays
// correctly excluded. Nothing in this package hands one Store to two goroutines,
// and nothing may start doing so. See [Store.Lock].
//
// # What a writer does when advisory locking is unavailable
//
// [ErrLockUnsupported] means NO protection at all: it is the only behaviour on
// GOOS=windows and is reachable on an NFS-mounted state root. The decision, made
// explicitly here rather than by omission, is to PROCEED UNSERIALIZED and warn
// (diag.CodeOwnershipLockUnavailable), not to refuse:
//
//   - Refusing would make every mutation on such a filesystem fail, i.e. make
//     the tool unusable exactly where flock is unavailable.
//   - Proceeding is byte-for-byte today's behaviour, on the only filesystems
//     where today's behaviour is unavoidable, so it is a documented degradation
//     rather than a new weakening.
//   - The portable backstop for precisely this path is the compare-and-swap
//     precondition on Apply (bead agent-marketplace-y1id / B5), which needs no
//     kernel support and turns the degraded case from a silent corruption into a
//     loud refusal.
//
// EVERY OTHER acquisition failure refuses the mutation and writes nothing: a
// timeout means another process is still inside its section, and a permission
// error or ENOSPC on the state root is the same condition under which the
// mandatory safety backup would fail anyway.
func (s *Store) withWriteLock(op string, fn func() error) error {
	return s.withWriteLockContext(context.Background(), op, fn)
}

// withWriteLockContext is [Store.withWriteLock] with cancellation.
func (s *Store) withWriteLockContext(ctx context.Context, op string, fn func() error) error {
	// WithLock, never the raw Lock pair. Lock returns a NIL releaser together
	// with a non-nil error deliberately — a no-op releaser would let a caller
	// that ignored the error believe it holds a lock it does not — so
	// `release, err := s.Lock(); defer release()` above the error check panics.
	// WithLock also guarantees fn does not run unless the lock is held.
	started := false
	err := s.WithLockContext(ctx, func() error {
		started = true
		return fn()
	})
	if started || !errors.Is(err, ErrLockUnsupported) {
		// `started` distinguishes "acquisition said unsupported" from "the body
		// returned an error that happens to wrap ErrLockUnsupported". Without it
		// the second case would rerun the mutation on the degraded path below,
		// and running a mutation twice is worse than any locking failure. No
		// body reachable today returns such an error — nothing inside
		// applyLocked, recoverLocked, or backupOnlyLocked touches the lock — so
		// the flag is what makes "fn runs exactly once on every path" a
		// guarantee of this function rather than a property of its three current
		// callers. TestWithWriteLockRunsTheBodyExactlyOnce pins it.
		return err
	}
	s.reportDegraded(op, err)
	return fn()
}

// reportDegraded warns ONCE per Store that this root is not serialized.
//
// Degradation is a property of the ROOT — of its filesystem — not of each
// mutation in it, and the report is written for a human deciding whether to move
// XDG_STATE_HOME. Before the section was widened it fired twice per mutation
// (once in acquireLock, once here), which under a per-file loop over a root's
// aggregates is 2N identical warnings for one fact; acquireLock's copy is now a
// debug fact and this one is deduplicated, so a degraded root reports exactly
// once per run. The whole message is kept intact — "NOT serialized" is what makes
// it greppable, and several tests search for exactly that.
func (s *Store) reportDegraded(op string, err error) {
	reported := false
	s.degradeOnce.Do(func() {
		reported = true
		slog.Warn("advisory locking is unavailable here, so this ownership mutation is NOT serialized against other processes",
			"code", diag.CodeOwnershipLockUnavailable,
			"root_key", s.rootKey, "canonical_root", s.canonicalRoot, "op", op, "err", err)
	})
	if !reported {
		slog.Debug("advisory locking is still unavailable for this root; the degradation was already reported for this run",
			"code", diag.CodeOwnershipLockUnavailable,
			"root_key", s.rootKey, "op", op)
	}
}

// releaser returns an idempotent release function for one depth level.
func (s *Store) releaser() func() {
	var once sync.Once
	return func() { once.Do(s.releaseOne) }
}

// releaseOne drops one depth level, closing the descriptor — and therefore
// letting the kernel drop the lock — only when the outermost level goes away.
//
// The lock FILE is never removed. See the unlink race documented at the top of
// this file.
func (s *Store) releaseOne() {
	s.acquireMu.Lock()
	defer s.acquireMu.Unlock()

	s.lockMu.Lock()
	if s.session == nil {
		s.lockMu.Unlock()
		return
	}
	s.session.depth--
	depth := s.session.depth
	var f *os.File
	if depth == 0 {
		f = s.session.f
		s.session = nil
	}
	s.lockMu.Unlock()

	if f != nil {
		defer func() {
			// Closing the descriptor is what releases the kernel lock; the explicit
			// LOCK_UN is belt and braces for the case where something else in this
			// process still holds a dup of the descriptor.
			dropHeldDescriptor(f)
			_ = unlockFile(f)
			_ = f.Close()
		}()
	}
	s.observeLock(LockEvent{Kind: LockReleased, Depth: depth})
}

// heldDescriptors keeps every descriptor whose kernel lock is currently held
// reachable from a package-level root.
//
// This is not bookkeeping, it is the fix for a silent release. A flock lives on
// the open file description, and the only reference to that description is the
// *os.File inside the Store's session — so a Store that becomes UNREACHABLE
// while a section is open (a caller that locks and then never mentions the store
// again, which is what a long body full of package-level calls looks like to the
// collector) has its descriptor closed by the runtime's file cleanup, and the
// kernel drops the lock with nobody informed. Another process then enters the
// section that is still running.
//
// Anchoring the descriptor here trades that silent loss of mutual exclusion for
// a loud one: a caller that drops a Store without releasing now leaks a HELD
// lock until the process exits, so the next writer on that root reports
// ownership.lock-timeout instead of quietly corrupting the record. Failing
// closed is the whole contract of this package, and a leaked fd is bounded by
// one per root per process.
var (
	heldDescriptorsMu sync.Mutex
	heldDescriptors   = map[*os.File]struct{}{}
)

func anchorHeldDescriptor(f *os.File) {
	heldDescriptorsMu.Lock()
	defer heldDescriptorsMu.Unlock()
	heldDescriptors[f] = struct{}{}
}

func dropHeldDescriptor(f *os.File) {
	heldDescriptorsMu.Lock()
	defer heldDescriptorsMu.Unlock()
	delete(heldDescriptors, f)
}

// acquireLock opens the lock file and takes the kernel lock, polling until the
// deadline. It returns the descriptor that owns the lock, how long it waited,
// and how many attempts it made.
func (s *Store) acquireLock(ctx context.Context) (*os.File, time.Duration, int, error) {
	path := s.LockPath()
	// An already-cancelled caller must not even create locks/: it asked to stop,
	// and the lock directory is the one thing acquisition adds to the state root.
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("ownership: lock %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, 0, 0, fmt.Errorf("ownership: create lock directory %s: %w", filepath.Dir(path), err)
	}
	// O_CREATE without O_EXCL: the file is created once per root and reused
	// forever. Recreating it would split the lock across two inodes.
	//
	// lockOpenFlags adds O_NOFOLLOW on unix. Acquisition TRUNCATES this path to
	// write its diagnostics-only metadata, so a symlink in the final component
	// would let anything that can write into locks/ redirect that truncation at
	// a file the lock does not own — including this package's own ownership
	// record two directories away. A symlinked lock path is refused loudly
	// instead: it is never something this package created.
	f, err := os.OpenFile(path, lockOpenFlags, 0o644)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("ownership: open lock file %s: %w", path, err)
	}
	if err := validateLockFile(f); err != nil {
		err = fmt.Errorf("ownership: validate lock file %s: %w", path, err)
		if closeErr := f.Close(); closeErr != nil {
			return nil, 0, 0, errors.Join(err, closeErr)
		}
		return nil, 0, 0, err
	}

	start := time.Now()
	deadline := start.Add(s.clampedLockTimeout())
	backoff := lockBackoffMin
	contendedReported := false

	try := tryLockFile
	if s.tryLock != nil {
		try = s.tryLock
	}

	for attempts := 1; ; attempts++ {
		lockErr := try(f)
		switch {
		case lockErr == nil:
			if err := validateLockFile(f); err != nil {
				err = fmt.Errorf("ownership: validate locked file %s: %w", path, err)
				if closeErr := f.Close(); closeErr != nil {
					return nil, time.Since(start), attempts, errors.Join(err, closeErr)
				}
				return nil, time.Since(start), attempts, err
			}
			waited := time.Since(start)
			if waited >= LockContendedReportThreshold {
				slog.Info("waited for another agent-marketplace process to release the ownership lock",
					"code", diag.CodeOwnershipLockContended,
					"root_key", s.rootKey,
					"waited", waited.String(),
					"attempts", attempts,
				)
			}
			return f, waited, attempts, nil

		case errors.Is(lockErr, ErrLockUnsupported):
			_ = f.Close()
			s.observeLock(LockEvent{Kind: LockUnsupported, Attempts: attempts, Err: lockErr})
			// DEBUG, not WARN. The user-facing report of this degradation belongs
			// to the writer that decides to proceed unserialized
			// (Store.reportDegraded, once per root); logging it here as well made
			// every degraded mutation warn twice, i.e. 2N times for a root with N
			// aggregate files. What is lost is nothing: a caller of the raw Lock
			// gets the coded error itself and decides what to say about it.
			slog.Debug("this filesystem does not support advisory locking; the caller decides whether to proceed unserialized",
				"code", diag.CodeOwnershipLockUnavailable,
				"root_key", s.rootKey,
				"err", lockErr,
			)
			return nil, time.Since(start), attempts, coded(diag.CodeOwnershipLockUnavailable, path, lockErr)

		case !errors.Is(lockErr, errLockBusy):
			_ = f.Close()
			return nil, time.Since(start), attempts, fmt.Errorf("ownership: lock %s: %w", path, lockErr)
		}

		// Contended.
		if !contendedReported {
			contendedReported = true
			s.observeLock(LockEvent{Kind: LockContended, Attempts: attempts})
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			_ = f.Close()
			waited := time.Since(start)

			if isENOLCK(lockErr) {
				s.observeLock(LockEvent{Kind: LockUnsupported, Waited: waited, Attempts: attempts, Err: ErrLockUnsupported})
				return nil, waited, attempts, coded(diag.CodeOwnershipLockUnavailable, path, ErrLockUnsupported)
			}

			s.observeLock(LockEvent{Kind: LockTimedOut, Waited: waited, Attempts: attempts, Err: ErrLockTimeout})
			slog.Error("could not acquire the ownership lock before the timeout; this target root was left exactly as found",
				"code", diag.CodeOwnershipLockTimeout,
				"root_key", s.rootKey,
				"waited", waited.String(),
				"attempts", attempts,
			)
			return nil, waited, attempts, coded(diag.CodeOwnershipLockTimeout, path,
				fmt.Errorf("%w after %s", ErrLockTimeout, waited.Round(time.Millisecond)))
		}
		// Cancellation is checked HERE, alongside the remaining time, rather than
		// only at the deadline: this is the one place the acquisition is idle, and
		// a caller that has been cancelled (Ctrl-C on a sync) must not be made to
		// wait out the rest of a 30s timeout for a lock it no longer wants.
		if err := sleepOrCancel(ctx, min(jitter(backoff), remaining)); err != nil {
			_ = f.Close()
			waited := time.Since(start)
			s.observeLock(LockEvent{Kind: LockCanceled, Waited: waited, Attempts: attempts, Err: err})
			slog.Debug("abandoned the wait for the ownership lock because the caller was cancelled",
				"root_key", s.rootKey, "waited", waited.String(), "attempts", attempts, "err", err)
			return nil, waited, attempts, fmt.Errorf("ownership: lock %s: %w", path, err)
		}
		backoff = min(backoff*2, lockBackoffMax)
	}
}

// sleepOrCancel waits for d or for ctx, whichever comes first, and reports
// ctx.Err() only in the latter case.
func sleepOrCancel(ctx context.Context, d time.Duration) error {
	if ctx.Done() == nil {
		// A context that can never be cancelled (context.Background) needs no
		// timer allocation on the contended path.
		time.Sleep(d)
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) clampedLockTimeout() time.Duration {
	if s.lockTimeout < 0 {
		return 0
	}
	return s.lockTimeout
}

// jitter spreads an interval by ±lockBackoffJitter so waiters do not
// resynchronize.
func jitter(d time.Duration) time.Duration {
	span := float64(d) * lockBackoffJitter
	out := time.Duration(float64(d) - span + 2*span*rand.Float64())
	if out < 0 {
		return 0
	}
	return out
}

func (s *Store) observeLock(ev LockEvent) {
	if s.lockObserver == nil {
		return
	}
	ev.RootKey = s.rootKey
	if ev.Path == "" {
		ev.Path = s.LockPath()
	}
	s.lockObserver(ev)
}

// writeLockMetadata records who holds the lock. It is written only AFTER the
// lock is held, it is best effort, and nothing ever reads it to make a decision
// — so a failed write cannot fail a mutation and a forged file cannot fool
// anything.
//
// The one reader is a human: the diag.CodeOwnershipLockTimeout error carries
// this file's path in its SourceRef (see acquireLock), so "another
// agent-marketplace process is still holding it" becomes actionable by
// `cat`ting the named path and finding the pid and argv0 of that process. If
// that path ever stops being reported to users, this write becomes dead weight
// and should be deleted rather than left as decoration.
func (s *Store) writeLockMetadata(f *os.File) {
	line := fmt.Sprintf("pid=%d host=%s argv0=%s acquired=%s\n",
		os.Getpid(), lockHost(), sanitizeLockField(filepath.Base(os.Args[0])),
		s.now().UTC().Format(time.RFC3339Nano))
	if err := validateLockFile(f); err != nil {
		slog.Debug("refused unsafe advisory lock metadata write", "root_key", s.rootKey, "err", err)
		return
	}
	if _, err := f.Seek(0, 0); err != nil {
		slog.Debug("could not rewind the advisory lock file for its metadata; the lock itself is unaffected",
			"root_key", s.rootKey, "err", err)
		return
	}
	if err := f.Truncate(0); err != nil {
		slog.Debug("could not truncate the advisory lock file; a previous holder's metadata may remain",
			"root_key", s.rootKey, "err", err)
		return
	}
	if _, err := f.WriteString(line); err != nil {
		slog.Debug("could not write advisory lock metadata; the lock itself is unaffected",
			"root_key", s.rootKey, "err", err)
	}
}

// lockHost resolves the hostname once per process. The hostname lookup is a
// syscall and the lock is on the sync latency path, so paying for it on every
// acquisition would be paying for a diagnostic nobody reads.
var lockHost = sync.OnceValue(func() string {
	host, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return sanitizeLockField(host)
})

// sanitizeLockField keeps the diagnostics-only lock file to one predictable
// line.
func sanitizeLockField(v string) string {
	v = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' {
			return '_'
		}
		return r
	}, v)
	if len(v) > 128 {
		return v[:128]
	}
	return v
}
