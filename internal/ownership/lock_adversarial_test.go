package ownership

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Adversarial companion to lock_test.go. Every test here was written to BREAK
// the primitive rather than to confirm it, and each one is a minimal repro of a
// single attack. As everywhere else in this package the roots are t.TempDir()
// roots and nothing ever touches a real ~/.local/state.

// ---------------------------------------------------------------------------
// Attack: break the reentrant session
// ---------------------------------------------------------------------------

// TestAdversarialConcurrentLockOnOneStoreDoesNotSelfContend constructs the race
// the reviewer flagged but did not repro: Lock dropped lockMu between the
// "session == nil" test and the store of the new session, so two goroutines on
// ONE Store could both decide to open their own descriptor. flock is per open
// file description, so the second descriptor contends with the FIRST ONE'S OWN,
// the loser waits out the whole timeout (30s by default), and the Store then
// reports ownership.lock-timeout — an Error whose message tells the user another
// agent-marketplace process is holding a lock this very Store is holding.
//
// The interleaving is CONSTRUCTED, not raced: the first goroutine is parked
// inside acquireLock, between the session check and the flock, by a test hook on
// the platform lock operation. The second goroutine then arrives at a Store
// whose session is still nil.
func TestAdversarialConcurrentLockOnOneStoreDoesNotSelfContend(t *testing.T) {
	f := newFixture(t)

	var (
		mu       sync.Mutex
		calls    int
		acquired int
		timedOut int
	)
	entered := make(chan int, 8)
	gate := make(chan struct{})

	st := f.store(
		WithLockTimeout(2*time.Second),
		WithClock(time.Now),
		WithLockObserver(func(ev LockEvent) {
			mu.Lock()
			defer mu.Unlock()
			switch ev.Kind {
			case LockAcquired:
				acquired++
			case LockTimedOut:
				timedOut++
			}
		}),
	)
	st.tryLock = func(file *os.File) error {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch n {
		case 1:
			// The first acquirer is now past the session check and holding its
			// own descriptor, but has not taken the kernel lock yet.
			entered <- n
			<-gate
		case 2:
			// Only a Store that let a second goroutine past the session check
			// can ever get here.
			entered <- n
		}
		return tryLockFile(file)
	}

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	releases := make(chan func(), 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		release, err := st.Lock()
		errs <- err
		if err == nil {
			releases <- release
		}
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first acquirer never reached the lock operation")
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		release, err := st.Lock()
		errs <- err
		if err == nil {
			releases <- release
		}
	}()

	// Wait for the second goroutine to reach the lock operation too. On a tree
	// where acquisition is properly serialized per Store it never will — it is
	// parked on the acquisition mutex and will take the reentrant path — so this
	// is a bounded wait for an event that must NOT happen, which is the one
	// place a timer is unavoidable. Nothing below depends on which branch runs.
	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
	}
	close(gate)

	wg.Wait()
	close(errs)
	close(releases)
	for err := range errs {
		if err != nil {
			t.Errorf("a second goroutine's Lock on a Store that already holds the lock failed with %v; "+
				"the documented contract is that a nested acquisition reuses the existing descriptor", err)
		}
	}
	for release := range releases {
		release()
	}
	if st.HoldsLock() {
		t.Fatalf("depth after releasing every acquisition = %d, want 0", st.LockDepth())
	}

	mu.Lock()
	defer mu.Unlock()
	if acquired != 1 {
		t.Errorf("two concurrent acquisitions on one Store opened %d open file descriptions, want 1; "+
			"every extra one is a descriptor no release will ever close", acquired)
	}
	if timedOut != 0 {
		t.Errorf("a Store reported %d ownership.lock-timeout events while holding its own lock", timedOut)
	}
}

// TestAdversarialOneStoreSharedByTwoGoroutinesIsNotMutualExclusion is a
// CHARACTERIZATION test, not a defect report: it pins that reentrancy is per
// STORE and therefore that a Store shared by two goroutines gives them NO mutual
// exclusion at all — the second acquisition is a depth bump, both bodies run at
// once, and the O_EXCL sentinel collides.
//
// It is here because B2 wires Lock into Apply. Any caller that hands one Store
// to two goroutines and expects the lock to serialize them is wrong in a way
// this test makes impossible to discover late.
func TestAdversarialOneStoreSharedByTwoGoroutinesIsNotMutualExclusion(t *testing.T) {
	f := newFixture(t)
	st := f.store(WithClock(time.Now))
	sentinel := filepath.Join(t.TempDir(), "inflight")

	outer, err := st.Lock()
	if err != nil {
		t.Fatalf("outer Lock: %v", err)
	}
	if err := occupySection(sentinel); err != nil {
		t.Fatalf("first section: %v", err)
	}
	// Re-enter it while the "first holder" has not released: a second goroutine
	// sharing this Store gets a releaser and believes it holds the lock.
	inner, err := st.Lock()
	if err != nil {
		t.Fatalf("nested Lock: %v", err)
	}
	if got := st.LockDepth(); got != 2 {
		t.Fatalf("depth = %d, want 2", got)
	}
	inner()
	// The inner release did NOT drop the kernel lock: only depth zero does.
	if !st.HoldsLock() {
		t.Fatal("releasing a nested level dropped the whole session")
	}
	other := f.store(WithLockTimeout(0))
	if _, err := other.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("a nested release ended the exclusive section: %v", err)
	}
	outer()
}

// TestAdversarialOutOfOrderAndRepeatedReleasesCannotCorruptDepth attacks the
// depth counter directly: releasing an outer level before an inner one, calling
// a releaser twice, releasing from a different goroutine, and using a releaser
// that belongs to a session that has already ended. None of these may close the
// descriptor while a level still believes it holds the lock, and none may leave
// the lock held after every level is gone.
func TestAdversarialOutOfOrderAndRepeatedReleasesCannotCorruptDepth(t *testing.T) {
	f := newFixture(t)
	st := f.store(WithClock(time.Now))
	probe := f.store(WithLockTimeout(0))

	outer, err := st.Lock()
	if err != nil {
		t.Fatalf("outer Lock: %v", err)
	}
	inner, err := st.Lock()
	if err != nil {
		t.Fatalf("inner Lock: %v", err)
	}

	// Out of order: the OUTER releaser first. One level goes, one remains, so
	// the descriptor must stay open and the section must stay exclusive.
	outer()
	if got := st.LockDepth(); got != 1 {
		t.Fatalf("depth after an out-of-order release = %d, want 1", got)
	}
	if _, err := probe.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("an out-of-order release dropped the kernel lock while a level still held it: %v", err)
	}

	// Repeated: the same releaser twice may not drop a level belonging to
	// somebody else.
	outer()
	if got := st.LockDepth(); got != 1 {
		t.Fatalf("depth after a double release = %d, want 1", got)
	}

	// From another goroutine: the release is not owned by the acquiring
	// goroutine, so this must simply work.
	done := make(chan struct{})
	go func() { defer close(done); inner() }()
	<-done
	if got := st.LockDepth(); got != 0 {
		t.Fatalf("depth after the last release = %d, want 0", got)
	}

	// A stale releaser from the finished session must not touch the NEXT one.
	next, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock for a second session: %v", err)
	}
	outer()
	inner()
	if got := st.LockDepth(); got != 1 {
		t.Fatalf("stale releasers from a previous session changed the new session's depth to %d, want 1", got)
	}
	next()
	release, err := probe.Lock()
	if err != nil {
		t.Fatalf("the lock was never released to another store: %v", err)
	}
	release()
}

// TestAdversarialPanicInsideWithLockStillReleases: WithLock defers the release,
// so a panicking section must not carry the lock out of the process's reach.
func TestAdversarialPanicInsideWithLockStillReleases(t *testing.T) {
	f := newFixture(t)
	st := f.store(WithClock(time.Now))

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the section did not panic, so this test proves nothing")
			}
		}()
		_ = st.WithLock(func() error { panic("boom") })
	}()

	if st.HoldsLock() {
		t.Fatal("a panicking section kept the lock")
	}
	other := f.store(WithLockTimeout(0))
	release, err := other.Lock()
	if err != nil {
		t.Fatalf("another store could not acquire after a panicking section: %v", err)
	}
	release()
}

// ---------------------------------------------------------------------------
// Attack: make the lock vanish while a caller believes it is held
// ---------------------------------------------------------------------------

// TestAdversarialGarbageCollectionDoesNotReleaseTheLock is the silent-release
// attack. A flock lives on the open file description, and the only reference to
// that description is the *os.File inside the Store's session — so if the Store
// becomes unreachable while a section is notionally open, the runtime's file
// cleanup closes the descriptor and the kernel drops the lock with nobody
// informed. A caller that takes the lock and then does work which does not
// mention the Store again is exactly that shape.
func TestAdversarialGarbageCollectionDoesNotReleaseTheLock(t *testing.T) {
	f := newFixture(t)
	targetRoot, stateRoot := f.targetRoot, f.stateRoot

	// The section: acquire, then let every reference to the Store and to the
	// releaser go out of scope while the section is still notionally running.
	func() {
		st, err := Open(targetRoot, WithStateRoot(stateRoot), WithClock(time.Now))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, err := st.Lock(); err != nil {
			t.Fatalf("Lock: %v", err)
		}
	}()

	// Give the runtime every chance to reclaim it. A real program does not call
	// runtime.GC(), but it does allocate, and a section that lasts longer than
	// one GC cycle is the normal case rather than the exotic one.
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}

	probe := f.store(WithLockTimeout(0))
	if _, err := probe.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("the lock was silently released by garbage collection while its section was still open: "+
			"a second store acquired it (%v)", err)
	}
}

// ---------------------------------------------------------------------------
// Attack: path aliasing — one logical root, two inodes; two roots, one inode
// ---------------------------------------------------------------------------

// TestAdversarialEquivalentSpellingsOfOneRootShareOneLock: a trailing slash, a
// "." segment, a ".." segment and a relative spelling must all key the same lock
// file. A spelling that produced a second lock path would produce a second inode
// and therefore two simultaneous holders of one logical root.
func TestAdversarialEquivalentSpellingsOfOneRootShareOneLock(t *testing.T) {
	f := newFixture(t)
	canonical := f.store()

	spellings := []string{
		f.targetRoot + string(filepath.Separator),
		filepath.Join(f.targetRoot, "."),
		filepath.Join(f.targetRoot, "..", ".devin"),
		f.targetRoot + "/./",
	}
	for _, spelling := range spellings {
		st, err := Open(spelling, WithStateRoot(f.stateRoot), WithClock(time.Now))
		if err != nil {
			t.Fatalf("Open(%q): %v", spelling, err)
		}
		if st.LockPath() != canonical.LockPath() {
			t.Fatalf("Open(%q) locks %q, but the canonical spelling locks %q: one root, two lock files, two holders",
				spelling, st.LockPath(), canonical.LockPath())
		}
	}

	// And the equivalence is real at the kernel: the canonical store's lock
	// genuinely excludes a store opened through an alias.
	release, err := canonical.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer release()
	aliased, err := Open(f.targetRoot+"/./", WithStateRoot(f.stateRoot), WithLockTimeout(0), WithClock(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aliased.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("an aliased spelling of the same root acquired the lock simultaneously: %v", err)
	}
}

// TestAdversarialSymlinkedStateRootStillExcludes: the STATE root is not
// canonicalized (WithStateRoot and XDG_STATE_HOME are both taken verbatim), so
// two Stores for one root can compute two different lock PATHS. That is
// harmless only because flock is per inode and both paths name the same file —
// which is the property this test pins, since the alternative would be two
// holders.
func TestAdversarialSymlinkedStateRootStillExcludes(t *testing.T) {
	f := newFixture(t)
	realState := f.stateRoot
	if err := os.MkdirAll(realState, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedState := filepath.Join(t.TempDir(), "linked-state")
	if err := os.Symlink(realState, linkedState); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	direct := f.store(WithClock(time.Now))
	viaLink, err := Open(f.targetRoot, WithStateRoot(linkedState), WithLockTimeout(0), WithClock(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	if direct.LockPath() == viaLink.LockPath() {
		t.Fatalf("the two spellings produced one path (%q), so this test proves nothing about inodes", direct.LockPath())
	}

	release, err := direct.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer release()
	if _, err := viaLink.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("a symlinked state root split the lock across two inodes; two holders at once: %v", err)
	}
}

// TestAdversarialSymlinkedLocksDirectoryStillExcludes: the same attack one level
// down, where locks/ itself is a symlink to a shared directory.
func TestAdversarialSymlinkedLocksDirectoryStillExcludes(t *testing.T) {
	f := newFixture(t)
	shared := filepath.Join(t.TempDir(), "shared-locks")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	secondState := filepath.Join(t.TempDir(), "state2", "agent-marketplace")
	if err := os.MkdirAll(secondState, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(f.stateRoot, lockDirName)); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	if err := os.Symlink(shared, filepath.Join(secondState, lockDirName)); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	first := f.store(WithClock(time.Now))
	second, err := Open(f.targetRoot, WithStateRoot(secondState), WithLockTimeout(0), WithClock(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	release, err := first.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer release()
	if _, err := second.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("two locks/ directories that are one directory did not exclude each other: %v", err)
	}
}

// TestAdversarialTwoDistinctRootsDoNotShareOneLock is the inverse attack: two
// genuinely different canonical roots must never collide onto one lock file,
// because that would serialize unrelated work and, worse, make a timeout on one
// root a report about the other.
func TestAdversarialTwoDistinctRootsDoNotShareOneLock(t *testing.T) {
	f := newFixture(t)
	otherRoot := filepath.Join(filepath.Dir(filepath.Dir(f.targetRoot)), "other-repo", ".devin")
	if err := os.MkdirAll(otherRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	other, err := Open(otherRoot, WithStateRoot(f.stateRoot), WithLockTimeout(0), WithClock(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	first := f.store(WithClock(time.Now))
	if first.LockPath() == other.LockPath() {
		t.Fatalf("two roots share the lock file %q", first.LockPath())
	}
	release, err := first.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer release()
	releaseOther, err := other.Lock()
	if err != nil {
		t.Fatalf("an unrelated root was blocked by another root's lock: %v", err)
	}
	releaseOther()
}

// TestAdversarialCaseVariantRootsSplitTheLock is a CHARACTERIZATION test of an
// inherited limitation, not a claim that this bead introduced it. CanonicalRoot
// deliberately does not fold case ("Case is deliberately NOT folded"), and
// doc.go documents the resulting SPLIT for ownership records. On a
// case-insensitive volume the consequence for the LOCK is sharper than for a
// record: /repo/.devin and /REPO/.devin are one directory but two root keys, so
// two concurrent syncs spelled differently hold two locks and serialize against
// nothing.
//
// The test asserts what is true today so the day someone folds case, or keys the
// lock by the directory's inode, this test tells them what changed.
func TestAdversarialCaseVariantRootsSplitTheLock(t *testing.T) {
	f := newFixture(t)
	upper := filepath.Join(filepath.Dir(filepath.Dir(f.targetRoot)), "REPO", ".devin")

	lower, err := os.Stat(f.targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	upperInfo, err := os.Stat(upper)
	if err != nil || !os.SameFile(lower, upperInfo) {
		t.Skip("this filesystem is case-sensitive, so the two spellings are genuinely two directories")
	}

	first := f.store(WithClock(time.Now))
	second, err := Open(upper, WithStateRoot(f.stateRoot), WithLockTimeout(0), WithClock(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	if first.LockPath() == second.LockPath() {
		// Somebody folded case: the split is gone and so is this hazard.
		return
	}
	release, err := first.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer release()
	releaseSecond, err := second.Lock()
	if err != nil {
		t.Fatalf("the case-different spelling is now excluded, which is BETTER than the documented behaviour; "+
			"update this test and doc.go's SPLIT paragraph together: %v", err)
	}
	releaseSecond()
	t.Log("known and documented: on a case-insensitive volume two case-different spellings of one directory " +
		"hold two independent locks (see CanonicalRoot's 'Case is deliberately NOT folded')")
}

// ---------------------------------------------------------------------------
// Attack: the metadata write
// ---------------------------------------------------------------------------

// TestAdversarialLockMetadataLeavesNoStaleTail: the write is
// Seek(0)+Truncate(0)+WriteString, so a lock file that already holds MORE bytes
// than the new line must not keep the tail — a reader would otherwise attribute
// a previous holder's pid to the current one.
func TestAdversarialLockMetadataLeavesNoStaleTail(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(filepath.Dir(st.LockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	junk := make([]byte, 8192)
	for i := range junk {
		junk[i] = 'x'
	}
	if err := os.WriteFile(st.LockPath(), junk, 0o644); err != nil {
		t.Fatal(err)
	}

	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	release()

	data, err := os.ReadFile(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) >= len(junk) {
		t.Fatalf("the metadata write left a stale tail: %d bytes", len(data))
	}
	// One line and nothing after it: a surviving tail would sit past the
	// newline. (A substring check would be fooled by a hostname that happens to
	// contain the filler byte.)
	if nl := bytes.IndexByte(data, '\n'); nl != len(data)-1 {
		t.Fatalf("previous contents survived the metadata write: %q", data)
	}
}

// TestAdversarialUnwritableLockFileFailsLoudlyAndHoldsNothing: an unwritable
// lock file must produce a hard error and NO lock. The unacceptable outcome is a
// Store that believes it holds a lock it does not.
func TestAdversarialUnwritableLockFileFailsLoudlyAndHoldsNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test relies on")
	}
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(filepath.Dir(st.LockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.LockPath(), nil, 0o444); err != nil {
		t.Fatal(err)
	}

	release, err := st.Lock()
	if err == nil {
		release()
		t.Fatal("acquisition succeeded on a lock file this user cannot open for writing")
	}
	if errors.Is(err, ErrLockTimeout) || errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("a permission failure was reported as %v; a caller would degrade or retry instead of failing", err)
	}
	if st.HoldsLock() {
		t.Fatal("the store believes it holds a lock it never took")
	}
}

// TestAdversarialLockPathOccupiedByADirectoryFailsLoudly: the same requirement
// when the lock path is not a regular file at all.
func TestAdversarialLockPathOccupiedByADirectoryFailsLoudly(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(st.LockPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	release, err := st.Lock()
	if err == nil {
		release()
		t.Fatal("acquisition succeeded with a directory in place of the lock file")
	}
	if errors.Is(err, ErrLockTimeout) || errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("a malformed lock path was reported as %v", err)
	}
	if st.HoldsLock() {
		t.Fatal("the store believes it holds a lock it never took")
	}
}

// TestAdversarialUnwritableLocksDirectoryFailsLoudly: a read-only locks/
// directory (or a read-only state root) must not be mistaken for "no lock
// needed".
func TestAdversarialUnwritableLocksDirectoryFailsLoudly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test relies on")
	}
	f := newFixture(t)
	st := f.store()
	dir := filepath.Dir(st.LockPath())
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	release, err := st.Lock()
	if err == nil {
		release()
		t.Fatal("acquisition succeeded in a directory this user cannot write")
	}
	if errors.Is(err, ErrLockTimeout) || errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("an unwritable locks directory was reported as %v", err)
	}
	if st.HoldsLock() {
		t.Fatal("the store believes it holds a lock it never took")
	}
}

// TestAdversarialMetadataWriteFailureStillHoldsTheLock: the metadata is
// diagnostics only, so a failure to write it must NOT lose the lock. The failure
// is manufactured by revoking write permission on the descriptor's file after it
// is opened, which on unix is not enough — so the check that matters is the
// positive one: the lock is still exclusive afterwards.
func TestAdversarialMetadataWriteFailureStillHoldsTheLock(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer release()

	// Whatever happened to the metadata, the lock is the descriptor, and the
	// descriptor is held.
	if !st.HoldsLock() {
		t.Fatal("the store lost its own session")
	}
	other := f.store(WithLockTimeout(0))
	if _, err := other.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("the lock is not exclusive after the metadata write: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Attack: starve or wedge an acquirer
// ---------------------------------------------------------------------------

// TestAdversarialTimeoutIsNotMateriallyExceeded: the deadline is checked before
// the sleep and the sleep is clamped to the remaining time, so a waiter must
// give up within a small constant of its timeout however long the holder holds.
func TestAdversarialTimeoutIsNotMateriallyExceeded(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer release()

	const timeout = 150 * time.Millisecond
	waiter := f.store(WithLockTimeout(timeout), WithClock(time.Now))
	start := time.Now()
	if _, err := waiter.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("want ErrLockTimeout, got %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < timeout {
		t.Fatalf("gave up after %s, before the %s deadline", elapsed, timeout)
	}
	// Generous on purpose: the signal is "it did not wait out a longer deadline",
	// not the exact wake-up latency of a loaded scheduler.
	if elapsed > timeout+10*lockBackoffMax {
		t.Fatalf("overshot the %s deadline by %s", timeout, elapsed-timeout)
	}
}

// TestAdversarialJitterNeverBusySpins: the backoff is jittered by ±20%, so a
// sleep can never be zero or negative and a waiter can never spin the CPU.
// Attempts are the observable: a bounded wait must make a bounded number of
// them.
func TestAdversarialJitterNeverBusySpins(t *testing.T) {
	for _, d := range []time.Duration{lockBackoffMin, lockBackoffMax, time.Nanosecond, 0} {
		for i := 0; i < 1000; i++ {
			if got := jitter(d); got < 0 {
				t.Fatalf("jitter(%s) = %s", d, got)
			}
		}
	}

	f := newFixture(t)
	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer release()

	var attempts int
	waiter := f.store(
		WithLockTimeout(500*time.Millisecond),
		WithClock(time.Now),
		WithLockObserver(func(ev LockEvent) {
			if ev.Kind == LockTimedOut {
				attempts = ev.Attempts
			}
		}),
	)
	if _, err := waiter.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("want ErrLockTimeout, got %v", err)
	}
	// 500ms of 25ms-and-doubling backoff is at most ~15 attempts; anything near
	// thousands means the sleep collapsed to zero.
	if attempts > 40 {
		t.Fatalf("a 500ms wait made %d attempts, so the backoff is not sleeping", attempts)
	}
	if attempts < 2 {
		t.Fatalf("a 500ms wait made %d attempts, so it never actually retried", attempts)
	}
}

// TestAdversarialZeroTimeoutNeverBlocks: a fail-fast waiter must return
// essentially instantly even against a holder that never lets go.
func TestAdversarialZeroTimeoutNeverBlocks(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer release()

	waiter := f.store(WithLockTimeout(0), WithClock(time.Now))
	start := time.Now()
	if _, err := waiter.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("want ErrLockTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 20*lockBackoffMin {
		t.Fatalf("a zero timeout blocked for %s", elapsed)
	}
}

// TestAdversarialManyWaitersAllEventuallyAcquire: acquisition has jitter but no
// queue, so nothing guarantees fairness. This pins the weaker property that
// actually matters — under sustained contention from N waiters against a holder
// that cycles, every waiter still completes inside its deadline rather than
// being starved indefinitely.
func TestAdversarialManyWaitersAllEventuallyAcquire(t *testing.T) {
	f := newFixture(t)
	const waiters = 8
	const rounds = 5

	start := make(chan struct{})
	errs := make(chan error, waiters)
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := f.store(WithLockTimeout(30*time.Second), WithClock(time.Now))
			<-start
			for r := 0; r < rounds; r++ {
				release, err := st.Lock()
				if err != nil {
					errs <- err
					return
				}
				release()
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("a waiter was starved out of its 30s deadline: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Attack: EverRecorded isolation
// ---------------------------------------------------------------------------

// TestAdversarialAcquisitionCreatesNothingButTheLockFile walks the whole state
// root after an acquire/release cycle. The existing tests check the two
// directories EverRecorded reads; this one checks that nothing ELSE appears
// either, so a future addition to the acquisition path cannot quietly create a
// directory that some later evidence rule starts reading.
func TestAdversarialAcquisitionCreatesNothingButTheLockFile(t *testing.T) {
	f := newFixture(t)
	st := f.store()

	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	release()

	var got []string
	err = filepath.WalkDir(st.StateRoot(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(st.StateRoot(), path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		got = append(got, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		lockDirName: true,
		filepath.Join(lockDirName, st.RootKey()+lockFileExt): true,
	}
	for _, entry := range got {
		if !want[entry] {
			t.Fatalf("acquiring the lock created %q under the state root; only locks/ may appear", entry)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("state root after one acquisition = %v, want exactly %d entries", got, len(want))
	}
}

// TestAdversarialStateRootPointedAtTheOwnershipDirectoryIsStillSilent: the
// nastiest spelling of the F4 trap. If a user points XDG_STATE_HOME at a
// directory that already contains ownership records, locks/ lands next to them —
// and must still not be read as evidence for any root key.
func TestAdversarialStateRootPointedAtTheOwnershipDirectoryIsStillSilent(t *testing.T) {
	f := newFixture(t)
	outer := f.store()
	nested := filepath.Join(outer.StateRoot(), ownershipDirName)
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	inner, err := Open(f.targetRoot, WithStateRoot(nested), WithClock(time.Now))
	if err != nil {
		t.Fatal(err)
	}

	release, err := inner.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	release()

	for name, st := range map[string]*Store{"outer": outer, "inner": inner} {
		if st.EverRecorded() {
			t.Fatalf("%s store: a lock under %s became ownership evidence", name, nested)
		}
		if _, err := st.Load(); !errors.Is(err, ErrStateUninitialized) {
			t.Fatalf("%s store: Load = %v, want ErrStateUninitialized", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Attack: the failure contract itself
// ---------------------------------------------------------------------------

// TestAdversarialFailedLockReturnsANilReleaser pins the shape of every failure
// return, because B2 and B3 are about to write `release, err := store.Lock()` in
// two more places. Lock returns a NIL release function together with a non-nil
// error, so `defer release()` written above the error check is a nil-function
// panic rather than a no-op. That is the standard Go shape and the alternative
// (a non-nil no-op releaser) would be worse — it would let a caller that ignored
// the error believe it holds the lock — so this is pinned rather than changed,
// and WithLock exists so callers need not get it right.
func TestAdversarialFailedLockReturnsANilReleaser(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	releaseHolder, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer releaseHolder()

	release, err := f.store(WithLockTimeout(0)).Lock()
	if err == nil {
		t.Fatal("the waiter acquired a held lock")
	}
	if release != nil {
		t.Fatal("Lock returned a non-nil releaser together with an error; the two-value contract changed and " +
			"every call site's error handling must be re-read")
	}

	// And the trap is live: a caller that defers before checking panics.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("calling the nil releaser did not panic, so this test no longer describes the contract")
			}
		}()
		release()
	}()
}

// TestAdversarialWithLockDoesNotRunTheSectionWhenAcquisitionFails: the safe
// wrapper must never run fn without the lock, because a caller that ignores
// WithLock's error would otherwise have mutated the root unserialized.
func TestAdversarialWithLockDoesNotRunTheSectionWhenAcquisitionFails(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer release()

	ran := false
	err = f.store(WithLockTimeout(0)).WithLock(func() error {
		ran = true
		return nil
	})
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("WithLock returned %v, want ErrLockTimeout", err)
	}
	if ran {
		t.Fatal("WithLock ran the critical section without the lock")
	}
}
