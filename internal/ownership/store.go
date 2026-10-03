package ownership

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CrashPoint names one boundary at which a real process can die mid-mutation.
// Every point is a place where the on-disk state changes meaning, and the plan
// requires that "Crash-injection tests cover every boundary between backup,
// journal fsync, aggregate rename, state commit, and journal cleanup".
//
// These constants exist in PRODUCTION code, not in a test helper, so a crash is
// injected at exactly the instruction the real code would die at rather than
// simulated by a mock that approximates the sequence.
type CrashPoint string

const (
	// CrashBeforeBackup: nothing has been written at all.
	CrashBeforeBackup CrashPoint = "before-backup"
	// CrashAfterBackup: the raw pre-mutation bytes are retained, but no journal
	// exists, so recovery has nothing to replay.
	CrashAfterBackup CrashPoint = "after-backup"
	// CrashAfterJournalPrepared: the journal is durable in phase "prepared".
	CrashAfterJournalPrepared CrashPoint = "after-journal-prepared"
	// CrashAfterStagedWrite: the patched bytes are durable in the staging file
	// but the live aggregate is untouched.
	CrashAfterStagedWrite CrashPoint = "after-staged-write"
	// CrashAfterAggregateRename: the live aggregate is the patched content, but
	// the journal still says "prepared" and state still records the old
	// generation.
	CrashAfterAggregateRename CrashPoint = "after-aggregate-rename"
	// CrashAfterJournalRenamed: the journal is durable in phase
	// "aggregate-renamed"; state still records the old generation.
	CrashAfterJournalRenamed CrashPoint = "after-journal-renamed"
	// CrashAfterStateCommit: file and state agree; only the journal is stale.
	CrashAfterStateCommit CrashPoint = "after-state-commit"
	// CrashAfterJournalCleanup: the generation is complete.
	CrashAfterJournalCleanup CrashPoint = "after-journal-cleanup"
)

// ErrInjectedCrash is returned by [Store.Apply] when a [Crasher] fires. It is
// deliberately distinguishable so a test can assert that the abort came from the
// injection point and not from an unrelated failure.
var ErrInjectedCrash = errors.New("ownership: injected crash")

// Crasher decides whether to abort at a boundary. Production code never
// installs one.
type Crasher interface {
	Crash(point CrashPoint) error
}

// CrashAt returns a Crasher that fires exactly once at the given point.
func CrashAt(point CrashPoint) Crasher { return &onceCrasher{point: point} }

type onceCrasher struct {
	point CrashPoint
	fired bool
}

func (c *onceCrasher) Crash(point CrashPoint) error {
	if c.fired || point != c.point {
		return nil
	}
	c.fired = true
	return fmt.Errorf("%w at %s", ErrInjectedCrash, point)
}

// Store is the durable ownership store for one canonical target root.
type Store struct {
	stateRoot          string
	canonicalStateRoot string
	canonicalRoot      string
	rootKey            string

	crasher Crasher
	now     func() time.Time
	// retain is the number of safety backups kept per canonical output file. The
	// plan fixes it at five; it is a field only so a test can prove the pruning
	// loop is driven by the constant rather than hard-coded comparisons.
	retain int

	// lockTimeout bounds how long [Store.Lock] waits for a contended per-root
	// advisory lock; lockObserver, when installed, reports every transition.
	lockTimeout  time.Duration
	lockObserver LockObserver
	// lockMu guards session only. session is the reentrancy state of THIS
	// Store: two Stores for one root deliberately hold two open file
	// descriptions and therefore genuinely contend. See lock.go.
	lockMu  sync.Mutex
	session *lockSession
	// acquireMu serializes whole acquisitions on this Store so that two
	// goroutines sharing it cannot both decide to open a descriptor. It is a
	// separate mutex from lockMu because it is held across the retry loop, and
	// LockDepth must stay answerable while somebody is waiting. See Store.Lock.
	acquireMu sync.Mutex
	// mutatingMu guards mutating, which records that a mutation body is in
	// flight on THIS Store. It is what refuses a nested or concurrent second
	// mutation, the one hazard the reentrant per-Store lock structurally cannot
	// exclude. See Store.withMutation and ErrNestedMutation.
	mutatingMu sync.Mutex
	mutating   bool
	// degradeOnce reports the ownership.lock-unavailable degradation at most
	// once per Store. A degraded root is a property of the ROOT, not of each
	// mutation in it: a widened session containing N sequential Applies would
	// otherwise log the same warning 1+N times, and the second acquisition
	// inside acquireLock would double that again. See Store.reportDegraded.
	degradeOnce sync.Once
	// tryLock overrides the platform's non-blocking flock. It is nil in
	// production and set only by tests in this package, which is the only way to
	// reach the ErrLockUnsupported branch on a filesystem that supports locking.
	tryLock func(*os.File) error
	// removeJournalFile overrides journal deletion for failure-path tests. It is
	// nil in production, where clearJournal uses os.Remove.
	removeJournalFile func(string) error
}

// Option customizes a Store.
type Option func(*Store)

// WithStateRoot overrides the XDG state root. Tests use it; production code
// relies on [StateRoot].
func WithStateRoot(dir string) Option {
	return func(s *Store) { s.stateRoot = dir }
}

// WithCrasher installs a crash injector.
func WithCrasher(c Crasher) Option {
	return func(s *Store) { s.crasher = c }
}

// WithClock overrides the clock used for timestamps.
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// SafetyBackupRetention is the number of safety backups retained per canonical
// output file: "Retain the five newest backups per canonical output file."
const SafetyBackupRetention = 5

// Open resolves the canonical root and its state key. It does not create
// anything on disk, so calling it on a read-only path (for example `status`) is
// side-effect free.
//
// Note what Open does NOT take: there is no backup-enabled flag and no Option
// that can disable safety backups. "Safety backups are mandatory even when
// configured backups are disabled" is enforced by the absence of the knob, not
// by a check on it — internal/backup's Config.Enabled governs only the
// timestamped tar.gz archives it owns and is not reachable from here.
func Open(targetRoot string, opts ...Option) (*Store, error) {
	canonical, err := CanonicalRoot(targetRoot)
	if err != nil {
		return nil, err
	}
	stateRoot, err := StateRoot()
	if err != nil {
		return nil, err
	}
	s := &Store{
		stateRoot:     stateRoot,
		canonicalRoot: canonical,
		rootKey:       RootKey(canonical),
		now:           time.Now,
		retain:        SafetyBackupRetention,
		lockTimeout:   DefaultLockTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.canonicalStateRoot, err = CanonicalRoot(s.stateRoot)
	if err != nil {
		return nil, err
	}
	// Env var > in-process configuration > code default, per AGENTS.md. The
	// resolution happens AFTER the options so the env var genuinely wins; see
	// [LockTimeoutEnvVar] for why that direction is the right one. A malformed
	// value fails Open rather than being ignored, which is loud on every command
	// instead of silent on the one command that needed the override.
	if d, ok, err := lockTimeoutFromEnv(); err != nil {
		return nil, err
	} else if ok {
		s.lockTimeout = d
	}
	return s, nil
}

// CanonicalRoot returns the canonical target root the store is keyed by.
func (s *Store) CanonicalRoot() string { return s.canonicalRoot }

// RootKey returns the state key for the canonical target root.
func (s *Store) RootKey() string { return s.rootKey }

// StateRoot returns the XDG state root in use.
func (s *Store) StateRoot() string { return s.stateRoot }

func (s *Store) recordDir() string {
	return filepath.Join(s.stateRoot, ownershipDirName, s.rootKey)
}

// StateFile returns the path of the authoritative ownership record.
func (s *Store) StateFile() string { return filepath.Join(s.recordDir(), stateFileName) }

// JournalFile returns the path of the transaction journal.
func (s *Store) JournalFile() string { return filepath.Join(s.recordDir(), journalFileName) }

// safetyRootDir returns the directory holding every safety backup taken for
// this canonical target root. Its existence is durable evidence that we once
// mutated a file under the root, which is what [Store.EverRecorded] reads.
func (s *Store) safetyRootDir() string {
	return filepath.Join(s.stateRoot, safetyDirName, s.rootKey)
}

// SafetyBackupDir returns the directory holding safety backups for one
// canonical output file.
func (s *Store) SafetyBackupDir(rel string) string {
	return filepath.Join(s.safetyRootDir(), fileSlug(rel))
}

func (s *Store) crash(point CrashPoint) error {
	if s.crasher == nil {
		return nil
	}
	return s.crasher.Crash(point)
}

// atomicWrite writes data to path atomically and durably: a temp file in the
// same directory, fsync of the file, rename, then fsync of the directory so the
// rename itself survives a power loss.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("ownership: create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("ownership: write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("ownership: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("ownership: fsync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("ownership: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("ownership: rename %s: %w", tmpName, err)
	}
	return fsyncDir(dir)
}

// fsyncDir flushes a directory entry so a rename or unlink is durable.
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("ownership: open dir %s: %w", dir, err)
	}
	defer func() { _ = f.Close() }()
	// Some filesystems refuse fsync on a directory. That is not a reason to
	// fail the whole mutation: the rename is still ordered, we simply cannot
	// prove it reached the platter.
	_ = f.Sync()
	return nil
}
