package ownership

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// Every test here uses t.TempDir() roots and never a real ~/.local/state, and
// none of them synchronizes with a sleep: in-process racers rendezvous on a
// channel, and cross-process racers rendezvous on pipes. Where a test needs one
// holder to be genuinely inside the section while another arrives, the holder is
// made to block on a pipe read rather than on a timer, so the interleaving is
// CONSTRUCTED rather than hoped for.

// ---------------------------------------------------------------------------
// Mutual exclusion in one process
// ---------------------------------------------------------------------------

// TestLockTwoStoresInOneProcessNeverHoldItSimultaneously is the in-process half
// of the acceptance criterion. It is a real test of the mechanism rather than of
// an in-process mutex because a flock is associated with the OPEN FILE
// DESCRIPTION: two Stores hold two descriptions and contend in the kernel even
// though they share an address space. A fix implemented as a package-level
// sync.Mutex would also pass this one, which is why the exec'd sibling below
// exists.
func TestLockTwoStoresInOneProcessNeverHoldItSimultaneously(t *testing.T) {
	f := newFixture(t)
	first := f.store()
	second := f.store(WithLockTimeout(0))

	release, err := first.Lock()
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if !first.HoldsLock() {
		t.Fatal("first store does not report holding the lock")
	}

	if _, err := second.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("second Lock while the first holds it: want ErrLockTimeout, got %v", err)
	}
	if second.HoldsLock() {
		t.Fatal("second store reports holding a lock it never acquired")
	}

	release()
	if first.HoldsLock() {
		t.Fatal("first store still reports holding the lock after release")
	}

	releaseSecond, err := second.Lock()
	if err != nil {
		t.Fatalf("second Lock after release: %v", err)
	}
	releaseSecond()
}

// TestLockSerializesConcurrentStoresInOneProcess proves mutual exclusion by
// proving a single positive event never occurs, rather than by sampling for an
// absence of corruption. Each racer creates an O_EXCL sentinel on entry and
// removes it on exit: if two sections overlap by one instruction the second
// create fails with EEXIST and the test fails immediately, with no dependence on
// scheduling luck.
func TestLockSerializesConcurrentStoresInOneProcess(t *testing.T) {
	f := newFixture(t)
	sentinel := filepath.Join(t.TempDir(), "inflight")

	const racers = 8
	start := make(chan struct{})
	errs := make(chan error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A real clock, not the fixture's deterministic one: the fixture clock
			// is a plain field and these racers genuinely run in parallel.
			st := f.store(WithLockTimeout(30*time.Second), WithClock(time.Now))
			<-start
			errs <- st.WithLock(func() error { return occupySection(sentinel) })
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("racer: %v", err)
		}
	}
	// The lock file survives every acquisition: it is created once per root and
	// never unlinked, because unlink-and-recreate would split the lock across two
	// inodes.
	if _, err := os.Stat(f.store().LockPath()); err != nil {
		t.Fatalf("lock file after %d acquisitions: %v", racers, err)
	}
}

// occupySection is the O_EXCL sentinel: exclusive creation is the kernel's own
// answer to "was anybody else already in here?".
func occupySection(path string) error {
	sf, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("two critical sections overlapped: %w", err)
	}
	if err := sf.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}

// ---------------------------------------------------------------------------
// Mutual exclusion across processes
// ---------------------------------------------------------------------------

const (
	lockChildModeEnv  = "AM_OWNERSHIP_LOCK_CHILD_MODE"
	lockChildRootEnv  = "AM_OWNERSHIP_LOCK_CHILD_TARGET_ROOT"
	lockChildStateEnv = "AM_OWNERSHIP_LOCK_CHILD_STATE_ROOT"
	lockChildSentinel = "AM_OWNERSHIP_LOCK_CHILD_SENTINEL"

	// Tokens the child writes on stdout. The parent rendezvouses on these rather
	// than on a timer.
	childAcquired = "ACQUIRED"
	childReleased = "RELEASED"
	childTimeout  = "TIMEOUT"
	childOverlap  = "OVERLAP"
	childReady    = "READY"
)

// TestLockChildProcess is not a test: it is the body of the child process the
// cross-process tests exec. It is inert unless the mode env var is set, so a
// normal `go test` run skips it.
func TestLockChildProcess(t *testing.T) {
	mode := os.Getenv(lockChildModeEnv)
	if mode == "" {
		t.Skip("not a child process invocation")
	}
	st, err := Open(os.Getenv(lockChildRootEnv),
		WithStateRoot(os.Getenv(lockChildStateEnv)),
		WithLockTimeout(childTimeoutFor(mode)),
	)
	if err != nil {
		t.Fatalf("child Open: %v", err)
	}

	switch mode {
	case "hold":
		// Acquire, announce, and block on stdin until the parent releases us. The
		// block is a pipe read, so the parent controls the interleaving exactly.
		release, err := st.Lock()
		if err != nil {
			t.Fatalf("child Lock: %v", err)
		}
		fmt.Println(childAcquired)
		_, _ = os.Stdin.Read(make([]byte, 1))
		release()
		fmt.Println(childReleased)

	case "try":
		release, err := st.Lock()
		if errors.Is(err, ErrLockTimeout) {
			fmt.Println(childTimeout)
			return
		}
		if err != nil {
			t.Fatalf("child Lock: %v", err)
		}
		release()
		fmt.Println(childAcquired)

	case "sentinel":
		// Announce readiness and block on a pipe read until the parent releases
		// every sibling at once. Without that rendezvous the children would reach
		// their sections tens of milliseconds apart — process startup dominates a
		// microsecond-long section — and the sentinel below would never collide
		// even on a tree with no locking at all.
		fmt.Println(childReady)
		_, _ = os.Stdin.Read(make([]byte, 1))
		// Acquire and take the O_EXCL sentinel: if any other process is inside its
		// section at the same instant, exclusive creation fails and says so.
		if err := st.WithLock(func() error { return occupySection(os.Getenv(lockChildSentinel)) }); err != nil {
			fmt.Println(childOverlap, err)
			t.Fatalf("child section: %v", err)
		}
		fmt.Println(childAcquired)

	default:
		t.Fatalf("unknown child mode %q", mode)
	}
}

func childTimeoutFor(mode string) time.Duration {
	if mode == "try" {
		// Fail fast: the parent guarantees the lock is held, so any wait at all
		// would only slow the test down.
		return 0
	}
	return 30 * time.Second
}

// childCmd builds an invocation of this test binary as a lock child.
func childCmd(t *testing.T, mode, targetRoot, stateRoot string, extraEnv ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockChildProcess$", "-test.timeout=120s")
	cmd.Env = append(os.Environ(),
		lockChildModeEnv+"="+mode,
		lockChildRootEnv+"="+targetRoot,
		lockChildStateEnv+"="+stateRoot,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

// startHolder execs a child that acquires the lock and blocks until told to let
// go. It returns the process and a function that releases it and waits.
func startHolder(t *testing.T, targetRoot, stateRoot string) (*exec.Cmd, func()) {
	t.Helper()
	cmd := childCmd(t, "hold", targetRoot, stateRoot)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	// A holder blocks on a pipe read, so a t.Fatal anywhere below would otherwise
	// leave it alive holding the lock until its own -test.timeout expires.
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	scanner := bufio.NewScanner(stdout)
	if !scanLineContaining(scanner, childAcquired) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("holder child never reported acquiring the lock")
	}
	return cmd, func() {
		_, _ = stdin.Write([]byte("\n"))
		_ = stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("holder child exited with error: %v", err)
		}
	}
}

func scanLineContaining(scanner *bufio.Scanner, token string) bool {
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), token) {
			return true
		}
	}
	return false
}

// TestLockTwoExecdProcessesNeverHoldItSimultaneously is the acceptance test the
// in-process one cannot be: the two contenders are real OS processes, so a fix
// implemented as a process-global mutex fails here. The interleaving is
// constructed, not raced — the holder is provably inside its section (it said so
// on a pipe) when the second process attempts acquisition.
func TestLockTwoExecdProcessesNeverHoldItSimultaneously(t *testing.T) {
	f := newFixture(t)

	_, letGo := startHolder(t, f.targetRoot, f.stateRoot)

	// While the child provably holds it, a second process must not get it.
	out, err := childCmd(t, "try", f.targetRoot, f.stateRoot).CombinedOutput()
	if err != nil {
		t.Fatalf("try child: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), childTimeout) {
		t.Fatalf("a second process acquired a lock another process was holding; child said %q", out)
	}

	letGo()

	// And once the holder is gone, the same invocation succeeds — proving the
	// refusal above was contention and not a broken lock path.
	out, err = childCmd(t, "try", f.targetRoot, f.stateRoot).CombinedOutput()
	if err != nil {
		t.Fatalf("try child after release: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), childAcquired) {
		t.Fatalf("process could not acquire a released lock; child said %q", out)
	}
}

// TestLockSerializesExecdProcesses runs several real processes through the
// section with the O_EXCL sentinel installed in each of them, so an overlap of
// even one instruction is reported by the kernel rather than inferred.
//
// The rendezvous is what makes it non-vacuous. Every child announces readiness
// on a pipe and blocks before acquiring, so all of them are released within
// microseconds of each other; started without that barrier they would boot tens
// of milliseconds apart and their microsecond-long sections could never collide,
// so the test would pass on a tree with the flock removed entirely.
func TestLockSerializesExecdProcesses(t *testing.T) {
	f := newFixture(t)
	sentinel := filepath.Join(t.TempDir(), "inflight")

	const children = 4
	cmds := make([]*exec.Cmd, 0, children)
	stdins := make([]io.WriteCloser, 0, children)
	for i := 0; i < children; i++ {
		cmd := childCmd(t, "sentinel", f.targetRoot, f.stateRoot, lockChildSentinel+"="+sentinel)
		cmd.Stderr = os.Stderr
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatalf("sentinel child %d stdin: %v", i, err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("sentinel child %d stdout: %v", i, err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("start sentinel child %d: %v", i, err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		if !scanLineContaining(bufio.NewScanner(stdout), childReady) {
			t.Fatalf("sentinel child %d never reported readiness", i)
		}
		cmds = append(cmds, cmd)
		stdins = append(stdins, stdin)
	}
	// Every child is parked at its acquisition point; release them together.
	for _, stdin := range stdins {
		_, _ = stdin.Write([]byte("\n"))
		_ = stdin.Close()
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("sentinel child %d overlapped another process's critical section: %v", i, err)
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("sentinel outlived every section: %v", err)
	}
}

// TestLockIsReleasedWhenTheHolderIsSIGKILLed is the whole of the stale-lock
// story in one function. A flock belongs to the open file description, so the
// kernel drops it when the holder dies however it dies. That is why this package
// has no PID check, no timestamp threshold, and no break protocol — and why
// nobody may add one.
func TestLockIsReleasedWhenTheHolderIsSIGKILLed(t *testing.T) {
	f := newFixture(t)

	holder, _ := startHolder(t, f.targetRoot, f.stateRoot)

	// The lock is genuinely unavailable while the holder lives. Without this the
	// assertion after the kill would also pass on a tree with no locking at all,
	// which would make the whole stale-lock story untested.
	if _, err := f.store(WithLockTimeout(0)).Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("the live holder's lock was not exclusive, so the kill below proves nothing: %v", err)
	}

	if err := holder.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_ = holder.Wait() // the exit status of a killed process is not interesting

	// Fail fast: if the kernel had not released the lock, this would time out
	// rather than flake.
	st := f.store(WithLockTimeout(5 * time.Second))
	release, err := st.Lock()
	if err != nil {
		t.Fatalf("lock after the holder was SIGKILLed: %v", err)
	}
	release()

	// And the lock file itself is still there: killing a holder does not require
	// anybody to clean anything up.
	if _, err := os.Stat(st.LockPath()); err != nil {
		t.Fatalf("lock file after a killed holder: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

// TestLockTimeoutIsDistinguishable pins that a caller can tell "somebody else
// has it" apart from every other acquisition failure, because the two demand
// opposite responses: a timeout means skip this root and exit non-zero, an
// unsupported filesystem means proceed unserialized and warn.
func TestLockTimeoutIsDistinguishable(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer release()

	waiter := f.store(WithLockTimeout(60 * time.Millisecond))
	start := time.Now()
	_, err = waiter.Lock()
	elapsed := time.Since(start)

	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("want ErrLockTimeout, got %v", err)
	}
	if errors.Is(err, ErrLockUnsupported) {
		t.Fatal("a timeout must not be reportable as an unsupported filesystem")
	}
	if elapsed < 60*time.Millisecond {
		t.Fatalf("gave up after %s, before the %s deadline", elapsed, 60*time.Millisecond)
	}
	var ce *diag.CodedError
	if !errors.As(err, &ce) || ce.Code != diag.CodeOwnershipLockTimeout {
		t.Fatalf("want a %s coded error, got %v", diag.CodeOwnershipLockTimeout, err)
	}
	if ce.Severity != diag.SeverityError {
		t.Fatalf("%s must be an error severity, got %v", diag.CodeOwnershipLockTimeout, ce.Severity)
	}
}

// TestLockZeroTimeoutFailsFast pins the documented meaning of a zero timeout:
// exactly one attempt, no waiting at all.
func TestLockZeroTimeoutFailsFast(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer release()

	var events []LockEvent
	waiter := f.store(WithLockTimeout(0), WithLockObserver(func(ev LockEvent) { events = append(events, ev) }))
	if _, err := waiter.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("want ErrLockTimeout, got %v", err)
	}
	for _, ev := range events {
		if ev.Kind == LockTimedOut && ev.Attempts != 1 {
			t.Fatalf("a zero timeout must make exactly one attempt, made %d", ev.Attempts)
		}
	}
}

// TestLockNegativeTimeoutIsClampedToFailFast: a misconfigured negative duration
// must never be able to mean "wait forever".
func TestLockNegativeTimeoutIsClampedToFailFast(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer release()

	waiter := f.store(WithLockTimeout(-time.Hour))
	start := time.Now()
	if _, err := waiter.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("want ErrLockTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a negative timeout waited %s", elapsed)
	}
}

// ---------------------------------------------------------------------------
// Reentrancy
// ---------------------------------------------------------------------------

// TestLockIsReentrantPerStore pins the property that lets a caller widen its
// critical section around Recover plus N mutations without the inner
// acquisitions deadlocking. Reentrancy is per STORE, so the outer section is
// still exclusive against every other Store — which the mid-test probe asserts.
func TestLockIsReentrantPerStore(t *testing.T) {
	f := newFixture(t)
	var events []LockEvent
	st := f.store(WithLockObserver(func(ev LockEvent) { events = append(events, ev) }))

	outer, err := st.Lock()
	if err != nil {
		t.Fatalf("outer Lock: %v", err)
	}
	inner, err := st.Lock()
	if err != nil {
		t.Fatalf("nested Lock self-deadlocked or failed: %v", err)
	}
	deepest, err := st.Lock()
	if err != nil {
		t.Fatalf("third Lock: %v", err)
	}
	if got := st.LockDepth(); got != 3 {
		t.Fatalf("depth after three acquisitions = %d, want 3", got)
	}

	deepest()
	// Idempotent: a second call to the same release must not drop a level that
	// belongs to an outer scope.
	deepest()
	inner()
	if got := st.LockDepth(); got != 1 {
		t.Fatalf("depth after releasing two nested levels = %d, want 1", got)
	}

	// Still exclusive against a different Store while any level is held.
	other := f.store(WithLockTimeout(0))
	if _, err := other.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("a nested section stopped being exclusive: %v", err)
	}

	outer()
	if got := st.LockDepth(); got != 0 {
		t.Fatalf("depth after releasing every level = %d, want 0", got)
	}
	releaseOther, err := other.Lock()
	if err != nil {
		t.Fatalf("other Lock after the whole section closed: %v", err)
	}
	releaseOther()

	acquired, reentered := 0, 0
	for _, ev := range events {
		switch ev.Kind {
		case LockAcquired:
			acquired++
		case LockReentered:
			reentered++
		}
	}
	if acquired != 1 {
		t.Fatalf("a reentrant section opened %d file descriptions, want 1", acquired)
	}
	if reentered != 2 {
		t.Fatalf("reentered events = %d, want 2", reentered)
	}
}

// TestWithLockReleasesOnError pins that WithLock is not a leak when fn fails.
func TestWithLockReleasesOnError(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	sentinelErr := errors.New("boom")
	if err := st.WithLock(func() error { return sentinelErr }); !errors.Is(err, sentinelErr) {
		t.Fatalf("WithLock returned %v", err)
	}
	if st.HoldsLock() {
		t.Fatal("WithLock kept the lock after fn failed")
	}
	other := f.store(WithLockTimeout(0))
	release, err := other.Lock()
	if err != nil {
		t.Fatalf("another store could not acquire after a failed WithLock: %v", err)
	}
	release()
}

// ---------------------------------------------------------------------------
// Observer
// ---------------------------------------------------------------------------

// TestLockObserverReportsContentionWithoutSleeping shows the seam a later
// compare-and-swap bead uses to assert its precondition never fires on a locked
// path, and that a test can count contention deterministically.
func TestLockObserverReportsContentionWithoutSleeping(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	releaseHolder, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}

	seen := make(chan LockEvent, 16)
	waiter := f.store(
		WithLockTimeout(30*time.Second),
		WithClock(time.Now),
		WithLockObserver(func(ev LockEvent) { seen <- ev }),
	)

	done := make(chan error, 1)
	go func() {
		release, err := waiter.Lock()
		if err == nil {
			release()
		}
		done <- err
	}()

	// The waiter reports contention before it can possibly succeed, so the
	// handoff below is ordered by the channel rather than by a timer.
	ev := <-seen
	if ev.Kind != LockContended {
		t.Fatalf("first observed event = %q, want %q", ev.Kind, LockContended)
	}
	if ev.RootKey != holder.RootKey() {
		t.Fatalf("event root key = %q, want %q", ev.RootKey, holder.RootKey())
	}
	if ev.Path != holder.LockPath() {
		t.Fatalf("event path = %q, want %q", ev.Path, holder.LockPath())
	}

	releaseHolder()
	if err := <-done; err != nil {
		t.Fatalf("waiter never acquired the released lock: %v", err)
	}
	// Every observer call happened before the goroutine reported on done, so the
	// channel can be closed and drained without any further synchronization.
	close(seen)
	acquired := false
	for ev := range seen {
		if ev.Kind != LockAcquired {
			continue
		}
		acquired = true
		if ev.Attempts < 2 {
			t.Fatalf("a contended acquisition reported %d attempts, so it never actually waited", ev.Attempts)
		}
		if ev.Waited <= 0 {
			t.Fatal("a contended acquisition reported a zero wait")
		}
	}
	if !acquired {
		t.Fatal("no acquired event was observed after the contention cleared")
	}
}

func TestLockReleaseObserverRunsBeforeKernelUnlock(t *testing.T) {
	t.Setenv(LockTimeoutEnvVar, "0")
	f := newFixture(t)
	probe := f.store(WithLockTimeout(0))
	var probeErr error
	holder := f.store(WithLockObserver(func(ev LockEvent) {
		if ev.Kind != LockReleased || ev.Depth != 0 {
			return
		}
		release, err := probe.Lock()
		probeErr = err
		if release != nil {
			release()
		}
	}))

	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	release()

	if !errors.Is(probeErr, ErrLockTimeout) {
		t.Fatalf("probe Lock from the release observer: want ErrLockTimeout, got %v", probeErr)
	}
	after, err := probe.Lock()
	if err != nil {
		t.Fatalf("probe Lock after release: %v", err)
	}
	after()
}

func TestLockReleaseSerializesSameStoreAcquisition(t *testing.T) {
	f := newFixture(t)
	serialized := false
	var st *Store
	st = f.store(WithLockObserver(func(ev LockEvent) {
		if ev.Kind != LockReleased || ev.Depth != 0 {
			return
		}
		if st.acquireMu.TryLock() {
			st.acquireMu.Unlock()
			return
		}
		serialized = true
	}))

	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	release()
	if !serialized {
		t.Fatal("a concurrent acquisition could enter while the Store was still releasing its prior session")
	}
}

// ---------------------------------------------------------------------------
// The lock must not become evidence of ownership
// ---------------------------------------------------------------------------

// TestLockDirectoryIsNotOwnershipEvidence is the regression test for the trap
// this bead was warned about. Store.EverRecorded decides whether a first-ever
// sync stays SILENT about a missing record: it reads the existence of the
// ownership directory and of the safety-backup directory as durable evidence
// that this root was once managed. If acquiring a lock could be mistaken for
// such evidence, every fresh install would start emitting an
// ownership.state-missing warning on its very first run.
func TestLockDirectoryIsNotOwnershipEvidence(t *testing.T) {
	f := newFixture(t)
	st := f.store()

	if st.EverRecorded() {
		t.Fatal("a fixture with nothing on disk already claims to have been recorded")
	}

	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	release()

	// The lock file really was created — otherwise the assertion below would be
	// vacuous.
	if _, err := os.Stat(st.LockPath()); err != nil {
		t.Fatalf("lock file was not created, so this test proves nothing: %v", err)
	}

	if st.EverRecorded() {
		t.Fatal("acquiring the lock made EverRecorded answer yes; a first-ever sync would now warn about missing ownership state")
	}

	// And the first-run reporting flavour is unchanged end to end.
	if _, err := st.Load(); !errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("Load after a lock acquisition: want ErrStateUninitialized, got %v", err)
	}
	if _, err := st.Proof(f.rel); !errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("Proof after a lock acquisition: want ErrStateUninitialized, got %v", err)
	}
}

// TestLockPathIsASiblingOfEveryEvidenceDirectory pins the structural reason the
// test above passes, so a future refactor that moves the lock under
// ownership/<rootKey>/ fails here with an explanation rather than silently
// regressing first-run reporting.
func TestLockPathIsASiblingOfEveryEvidenceDirectory(t *testing.T) {
	f := newFixture(t)
	st := f.store()

	lockPath := st.LockPath()
	if want := filepath.Join(st.StateRoot(), "locks", st.RootKey()+".lock"); lockPath != want {
		t.Fatalf("LockPath() = %q, want %q", lockPath, want)
	}
	for name, dir := range map[string]string{
		"ownership record directory": st.recordDir(),
		"safety backup directory":    st.safetyRootDir(),
	} {
		if strings.HasPrefix(lockPath, dir+string(filepath.Separator)) {
			t.Fatalf("the lock file lives inside the %s (%s); EverRecorded would read it as evidence", name, dir)
		}
	}

	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	release()
	for name, dir := range map[string]string{
		"ownership record directory": st.recordDir(),
		"safety backup directory":    st.safetyRootDir(),
	} {
		if _, err := os.Lstat(dir); !os.IsNotExist(err) {
			t.Fatalf("acquiring the lock created the %s (%s): %v", name, dir, err)
		}
	}
}

// TestReleaseNeverUnlinksTheLockFile pins the anti-reaper rule. Unlinking and
// recreating the lock file splits the lock across two inodes: one process holds
// the old inode, another creates and locks a new one, and both believe they hold
// "the" lock.
func TestReleaseNeverUnlinksTheLockFile(t *testing.T) {
	f := newFixture(t)
	st := f.store()

	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	before, err := os.Stat(st.LockPath())
	if err != nil {
		t.Fatalf("stat while held: %v", err)
	}
	release()

	after, err := os.Stat(st.LockPath())
	if err != nil {
		t.Fatalf("the lock file was removed on release: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("the lock file was replaced across a release; the lock would be split across two inodes")
	}

	// A second cycle reuses the same inode too.
	release2, err := st.Lock()
	if err != nil {
		t.Fatalf("second Lock: %v", err)
	}
	release2()
	again, err := os.Stat(st.LockPath())
	if err != nil {
		t.Fatalf("stat after a second cycle: %v", err)
	}
	if !os.SameFile(before, again) {
		t.Fatal("a second acquisition used a different inode")
	}
}

// TestLockMetadataIsDiagnosticsOnly: the contents exist for a human reading an
// ownership.lock-timeout report, are written only after the lock is held, and
// are never read to make a decision.
func TestLockMetadataIsDiagnosticsOnly(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	release()

	data, err := os.ReadFile(st.LockPath())
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, fmt.Sprintf("pid=%d", os.Getpid())) {
		t.Fatalf("lock metadata does not name the holder: %q", body)
	}
	if strings.Count(body, "\n") != 1 {
		t.Fatalf("lock metadata is not one line: %q", body)
	}

	// Corrupting it changes nothing: nothing reads it to decide anything.
	if err := os.WriteFile(st.LockPath(), []byte("pid=999999999 host=elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	release2, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock with forged metadata: %v", err)
	}
	release2()
}

// ---------------------------------------------------------------------------
// Platform
// ---------------------------------------------------------------------------

// TestNonUnixBuildCompiles pins that `GOOS=windows go build ./...` keeps working
// for anyone whose editor runs cross-platform analysis. Windows is explicitly
// NOT a supported platform: the stub provides no serialization and reports
// ErrLockUnsupported, which is a documented degradation rather than an accident.
func TestNonUnixBuildCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compilation is slower than a unit test")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root := moduleRoot(t)
	for _, goos := range []string{"windows", "linux", "darwin"} {
		if goos == runtime.GOOS {
			continue
		}
		cmd := exec.Command(goBin, "build", "./internal/ownership/...")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("GOOS=%s build failed: %v\n%s", goos, err, out)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

// TestLockUnsupportedIsDistinguishable pins the vocabulary the degraded path
// depends on, independent of whether this machine can produce the errno: an
// unsupported lock is not a timeout, so a caller cannot conflate "proceed and
// warn" with "skip this root and fail".
func TestLockUnsupportedIsDistinguishable(t *testing.T) {
	wrapped := fmt.Errorf("%w: operation not supported", ErrLockUnsupported)
	if !errors.Is(wrapped, ErrLockUnsupported) {
		t.Fatal("a wrapped ErrLockUnsupported is not recognizable")
	}
	if errors.Is(wrapped, ErrLockTimeout) {
		t.Fatal("ErrLockUnsupported must not be mistakable for ErrLockTimeout")
	}
	if _, ok := diag.DocForCode(diag.CodeOwnershipLockUnavailable); !ok {
		t.Fatalf("%s is not in the diagnostic catalog", diag.CodeOwnershipLockUnavailable)
	}
}

// TestLockDiagnosticCodesHaveTheDesignedSeverities pins the severities the
// design argues for: contention is Info so a save hook racing a human does not
// inflate the warning count --strict keys off, a timeout is an Error because a
// root was not reconciled, and an unsupported filesystem is a Warning because
// the mutation still happens.
func TestLockDiagnosticCodesHaveTheDesignedSeverities(t *testing.T) {
	for code, want := range map[string]diag.Severity{
		diag.CodeOwnershipLockContended:   diag.SeverityInfo,
		diag.CodeOwnershipLockTimeout:     diag.SeverityError,
		diag.CodeOwnershipLockUnavailable: diag.SeverityWarning,
	} {
		doc, ok := diag.DocForCode(code)
		if !ok {
			t.Fatalf("%s is not documented", code)
		}
		if doc.Severity != want {
			t.Fatalf("%s severity = %v, want %v", code, doc.Severity, want)
		}
	}
	// The design explicitly rejects a stale-reclaimed code: with flock there is
	// no such event, so a code for it could only ever be dead vocabulary.
	for _, code := range diag.AllCodes() {
		if code == "ownership.lock-stale-reclaimed" {
			t.Fatal("ownership.lock-stale-reclaimed exists; a flock cannot go stale, so nothing can emit it")
		}
	}
}
