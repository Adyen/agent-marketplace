package config

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// probeWaitTimeout bounds every wait in this file. It is deliberately far
// larger than any real interleaving needs: the point is to turn a deadlock into
// a reported failure instead of a hung test binary.
const probeWaitTimeout = 10 * time.Second

// pollInterval is how often the lock-free observations in this file re-check
// their condition. Small enough that no test pays a visible settle cost, large
// enough that the polling goroutine does not starve the goroutines it waits for.
const pollInterval = 200 * time.Microsecond

// installGitRootProbe substitutes a deterministic probe for the duration of the
// test and clears the memo on both sides, so nothing is inherited from or
// leaked to a sibling test (including a second `-count=2` pass).
//
// Both resets go through the non-blocking path. A blocking reset would defeat
// the whole file: on the way in it would inherit a previous test's wedged lock,
// and on the way out it runs from t.Cleanup after a t.Fatal, where blocking
// swallows the bounded diagnostic that was just reported and replaces it with a
// test-binary timeout panic minutes later.
func installGitRootProbe(t *testing.T, probe func(dir string) (string, bool)) {
	t.Helper()
	previous := gitRootProbe
	if !tryResetRepoRootCache() {
		t.Fatalf("gitRootMu could not be acquired: the memo is holding it across a wait, so this test cannot start from a clean memo")
	}
	gitRootProbe = probe
	t.Cleanup(func() {
		gitRootProbe = previous
		if !tryResetRepoRootCache() {
			t.Errorf("gitRootMu could not be acquired after the test: the memo is holding it across a wait, so the cache could not be cleared for the next test")
		}
	})
}

// memoWedged records that a helper gave up on gitRootMu, or that a bounded wait
// on the memo expired. A lock nobody will ever release stays unreleased for the
// life of the binary, so once one test has established that, every later test that
// goes through these helpers fails on that fact immediately instead of paying the
// bound again — the verdict is already known, and re-deriving it per helper call
// only buries the diagnostic.
//
// It cannot save the whole package: a test elsewhere in internal/config that
// resolves a repo root through FindRepoRoot blocks on the wedged lock with no
// bound available to it, so a wedge that survives into a second `-count` pass ends
// in the whole-package timeout panic. The diagnostics from this file are flushed
// before that panic, and TEST_RACE_TIMEOUT keeps the tail short.
var memoWedged atomic.Bool

// tryAcquireMemoLock takes gitRootMu without blocking indefinitely, reporting
// whether it succeeded. Every helper in this file that reads memo internals goes
// through it, because the failure these tests exist to catch is exactly
// "gitRootMu is held forever": a plain Lock() there converts a reportable
// failure into a hung binary.
func tryAcquireMemoLock() bool {
	if memoWedged.Load() {
		return false
	}
	deadline := time.Now().Add(probeWaitTimeout)
	for {
		if gitRootMu.TryLock() {
			return true
		}
		if time.Now().After(deadline) {
			memoWedged.Store(true)
			return false
		}
		time.Sleep(pollInterval)
	}
}

// tryResetRepoRootCache is ResetRepoRootCache with a bounded acquisition. It
// reports false rather than waiting forever for a wedged lock.
func tryResetRepoRootCache() bool {
	if !tryAcquireMemoLock() {
		return false
	}
	defer gitRootMu.Unlock()
	clear(gitRootCache)
	return true
}

// inflightProbeCount reports how many probes the memo currently believes are
// running. A non-zero count after every caller has returned means the memo
// leaks bookkeeping and would deadlock or re-probe forever.
func inflightProbeCount(t *testing.T) int {
	t.Helper()
	if !tryAcquireMemoLock() {
		t.Fatalf("gitRootMu could not be acquired to read the in-flight records: the memo is holding it across a wait")
	}
	defer gitRootMu.Unlock()
	return len(gitRootInflight)
}

// awaitQueuedWaiters blocks until at least want callers have queued behind an
// in-flight probe since base, and fails with a bounded diagnostic if they never
// do.
//
// This replaces a fixed settle sleep. The tests here need the followers to have
// actually reached the memo's critical section before the probe is released,
// otherwise they would pass against the cyclic implementation they exist to
// reject; waiting for the observation itself makes that ordering a fact rather
// than a bet on a timer, and removes the sleep from the fast path. The counter is
// read without gitRootMu on purpose: a memo that wedges the lock must still be
// able to produce this failure message.
func awaitQueuedWaiters(t *testing.T, base, want int64) {
	t.Helper()
	deadline := time.Now().Add(probeWaitTimeout)
	for {
		got := gitRootQueued.Load() - base
		if got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d callers queued behind the in-flight probe within %s", got, want, probeWaitTimeout)
		}
		time.Sleep(pollInterval)
	}
}

// awaitProbeEntry waits for a substituted probe to signal that it is running,
// so a test can be sure the leader exists before it starts followers.
func awaitProbeEntry(t *testing.T, entered <-chan struct{}, dir string) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(probeWaitTimeout):
		t.Fatalf("no goroutine reached the git probe for %s within %s", dir, probeWaitTimeout)
	}
}

// awaitRoot waits for one lookup result, failing with context instead of
// hanging when the lookup is wedged.
func awaitRoot(t *testing.T, results <-chan string, want, whatFailed string) {
	t.Helper()
	select {
	case got := <-results:
		if got != want {
			t.Fatalf("%s = %q; want %q", whatFailed, got, want)
		}
	case <-time.After(probeWaitTimeout):
		t.Fatalf("%s never completed within %s: the lookup is deadlocked", whatFailed, probeWaitTimeout)
	}
}

// awaitGoroutines waits for wg with a bound. A bare wg.Wait() is the one wait a
// test cannot recover from: unlike the bounded helpers here, it never returns
// against a wedged memo, so the failure is reported by the whole-package timeout
// panic long after the fact instead of by the test that caused it.
func awaitGoroutines(t *testing.T, wg *sync.WaitGroup, whatFailed string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(probeWaitTimeout):
		memoWedged.Store(true)
		t.Fatalf("%s did not finish within %s: the memo is deadlocked", whatFailed, probeWaitTimeout)
	}
}

// awaitReset calls the exported ResetRepoRootCache with a bounded wait. Tests
// that need the reset's effect go through it rather than tryResetRepoRootCache,
// which clears the map itself and so would assert nothing about the exported
// function; tests that only need a clean memo use the bounded helper.
func awaitReset(t *testing.T, when string) {
	t.Helper()
	if memoWedged.Load() {
		t.Fatalf("gitRootMu is already wedged by an earlier test, so ResetRepoRootCache cannot be exercised %s", when)
	}
	returned := make(chan struct{})
	go func() {
		ResetRepoRootCache()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(probeWaitTimeout):
		memoWedged.Store(true)
		t.Fatalf("ResetRepoRootCache did not return within %s %s: gitRootMu is held across a wait", probeWaitTimeout, when)
	}
}

// TestGitRootForSingleFlightsConcurrentSameDirectoryLookups is the regression
// test for the loader lock-order cycle. Concurrent lookups for one directory
// must collapse into a single probe AND all return, while the probe is still
// running when the followers arrive.
//
// An implementation that takes the per-directory lock while holding the map
// lock deadlocks here: a follower pins the map lock waiting for the probing
// goroutine, and the probing goroutine needs the map lock to publish its answer.
func TestGitRootForSingleFlightsConcurrentSameDirectoryLookups(t *testing.T) {
	var calls atomic.Int64
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	installGitRootProbe(t, func(dir string) (string, bool) {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return "/repo", true
	})

	const lookups = 32
	queuedBefore := gitRootQueued.Load()
	results := make(chan string, lookups)
	var wg sync.WaitGroup
	for i := 0; i < lookups; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- gitRootFor("/repo")
		}()
	}

	awaitProbeEntry(t, entered, "/repo")
	// One goroutine leads the probe, so every other one must have queued behind
	// it before the probe is allowed to finish — that is the interleaving a
	// lock-order cycle cannot survive.
	awaitQueuedWaiters(t, queuedBefore, lookups-1)
	close(release)

	for i := 0; i < lookups; i++ {
		awaitRoot(t, results, "/repo", "concurrent gitRootFor(\"/repo\")")
	}
	awaitGoroutines(t, &wg, "the concurrent lookups of one directory")

	if got := calls.Load(); got != 1 {
		t.Fatalf("git probe ran %d times for %d concurrent lookups of one directory; want exactly 1", got, lookups)
	}
	if got := inflightProbeCount(t); got != 0 {
		t.Fatalf("%d in-flight probe records left behind; want 0", got)
	}
}

// TestGitRootForDoesNotSerializeUnrelatedDirectories pins the property the
// per-directory locking exists for: a lookup for one directory must not wait on
// an in-flight probe for a different directory. This is the user-visible
// symptom of the lock-order cycle — one slow `git rev-parse` freezing every
// other lookup in the process (the TUI runs them from concurrent tea.Cmds).
func TestGitRootForDoesNotSerializeUnrelatedDirectories(t *testing.T) {
	slowEntered := make(chan struct{}, 1)
	release := make(chan struct{})
	installGitRootProbe(t, func(dir string) (string, bool) {
		if dir == "/slow" {
			select {
			case slowEntered <- struct{}{}:
			default:
			}
			<-release
			return "/slow", true
		}
		return dir, true
	})

	queuedBefore := gitRootQueued.Load()
	slow := make(chan string, 1)
	go func() { slow <- gitRootFor("/slow") }()
	awaitProbeEntry(t, slowEntered, "/slow")

	// A same-directory follower is what pins the shared lock in a cyclic
	// implementation, so it has to have reached the memo's critical section
	// before the unrelated lookup is attempted.
	sameDir := make(chan string, 1)
	go func() { sameDir <- gitRootFor("/slow") }()
	awaitQueuedWaiters(t, queuedBefore, 1)

	unrelated := make(chan string, 1)
	go func() { unrelated <- gitRootFor("/fast") }()
	awaitRoot(t, unrelated, "/fast", "gitRootFor(\"/fast\") during an in-flight probe for /slow")

	close(release)
	awaitRoot(t, slow, "/slow", "gitRootFor(\"/slow\") leader")
	awaitRoot(t, sameDir, "/slow", "gitRootFor(\"/slow\") follower")

	if got := inflightProbeCount(t); got != 0 {
		t.Fatalf("%d in-flight probe records left behind; want 0", got)
	}
}

// TestResetRepoRootCacheReturnsDuringInFlightProbe pins the property that makes
// the memo's own test cleanup safe: a reset must complete while a probe is
// running and while other callers are queued behind it. An implementation that
// holds gitRootMu across the probe (or across a waiter's wait) blocks the reset
// for as long as the probe runs, which in a test means the cleanup of a failing
// test hangs the binary instead of reporting the failure.
func TestResetRepoRootCacheReturnsDuringInFlightProbe(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	installGitRootProbe(t, func(dir string) (string, bool) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return "/repo", true
	})

	queuedBefore := gitRootQueued.Load()
	leader := make(chan string, 1)
	go func() { leader <- gitRootFor("/repo") }()
	awaitProbeEntry(t, entered, "/repo")

	follower := make(chan string, 1)
	go func() { follower <- gitRootFor("/repo") }()
	awaitQueuedWaiters(t, queuedBefore, 1)

	// Deliberately the exported ResetRepoRootCache, not tryResetRepoRootCache:
	// this is the assertion that the exported reset itself does not block.
	awaitReset(t, "while a probe was in flight with a waiter queued behind it")

	close(release)
	// The reset does not cancel an in-flight probe, so both callers still get the
	// answer that probe observed.
	awaitRoot(t, leader, "/repo", "gitRootFor(\"/repo\") leader across a reset")
	awaitRoot(t, follower, "/repo", "gitRootFor(\"/repo\") follower across a reset")

	if got := inflightProbeCount(t); got != 0 {
		t.Fatalf("%d in-flight probe records left behind after a reset during a probe; want 0", got)
	}
}

// TestGitRootForFailedProbeIsRetriedNotMemoized keeps the "a failed probe is
// not a cacheable answer" rule intact through the concurrency change, and
// proves the bookkeeping for a failed probe is cleaned up so the retry can
// actually run instead of waiting on a completed probe forever.
func TestGitRootForFailedProbeIsRetriedNotMemoized(t *testing.T) {
	var calls atomic.Int64
	var succeed atomic.Bool
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	installGitRootProbe(t, func(dir string) (string, bool) {
		calls.Add(1)
		if succeed.Load() {
			return "/repo", true
		}
		entered <- struct{}{}
		<-release
		return "", false
	})

	const failing = 4
	queuedBefore := gitRootQueued.Load()
	results := make(chan string, failing)
	for i := 0; i < failing; i++ {
		go func() { results <- gitRootFor("/repo") }()
	}
	awaitProbeEntry(t, entered, "/repo")
	awaitQueuedWaiters(t, queuedBefore, failing-1)
	close(release)
	for i := 0; i < failing; i++ {
		awaitRoot(t, results, "", "gitRootFor(\"/repo\") with a failing probe")
	}
	if got := inflightProbeCount(t); got != 0 {
		t.Fatalf("%d in-flight probe records left behind after a failed probe; want 0", got)
	}

	// The transient failure must be retried, and the recovered answer memoized.
	succeed.Store(true)
	before := calls.Load()
	if got := gitRootFor("/repo"); got != "/repo" {
		t.Fatalf("gitRootFor after a failed probe = %q; want /repo (a failure must not be memoized)", got)
	}
	if got := gitRootFor("/repo"); got != "/repo" {
		t.Fatalf("second gitRootFor = %q; want the memoized /repo", got)
	}
	if got := calls.Load(); got != before+1 {
		t.Fatalf("probe ran %d extra times after recovery; want exactly 1", got-before)
	}
}

// TestGitRootForPanickingProbeDoesNotWedgeWaiters covers the failure mode a
// promise-style memo introduces: if the probing goroutine dies without
// publishing, every other caller for that directory waits forever. A panic in
// the probe must still release the waiters and must not memoize an answer.
func TestGitRootForPanickingProbeDoesNotWedgeWaiters(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var panicked atomic.Bool
	installGitRootProbe(t, func(dir string) (string, bool) {
		if !panicked.Swap(true) {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			panic("probe exploded")
		}
		return "/repo", true
	})

	queuedBefore := gitRootQueued.Load()
	leaderReturned := make(chan struct{})
	go func() {
		defer close(leaderReturned)
		defer func() { _ = recover() }()
		gitRootFor("/repo")
	}()
	awaitProbeEntry(t, entered, "/repo")

	waiter := make(chan string, 1)
	go func() { waiter <- gitRootFor("/repo") }()
	awaitQueuedWaiters(t, queuedBefore, 1)
	close(release)

	select {
	case <-leaderReturned:
	case <-time.After(probeWaitTimeout):
		t.Fatal("the panicking lookup never unwound")
	}
	awaitRoot(t, waiter, "", "gitRootFor(\"/repo\") waiting on a panicking probe")

	if got := inflightProbeCount(t); got != 0 {
		t.Fatalf("%d in-flight probe records left behind after a panicking probe; want 0", got)
	}
	// Nothing was memoized, so the next caller re-probes and gets a real answer.
	if got := gitRootFor("/repo"); got != "/repo" {
		t.Fatalf("gitRootFor after a panicking probe = %q; want /repo", got)
	}
}

// TestGitRootForMixedConcurrentLookups is the stress case for `go test -race`:
// many goroutines, overlapping and distinct directories, and probes that
// sometimes fail. It asserts answers stay consistent, the memo does not leak
// bookkeeping, and no directory is probed more than the number of callers that
// could legitimately have missed the cache.
func TestGitRootForMixedConcurrentLookups(t *testing.T) {
	var failures atomic.Int64
	installGitRootProbe(t, func(dir string) (string, bool) {
		// Every third probe reports a transient failure, which is the branch
		// that must not memoize and must still release its waiters.
		if failures.Add(1)%3 == 0 {
			return "", false
		}
		return dir + "-root", true
	})

	dirs := []string{"/a", "/b", "/c", "/d"}
	const goroutines = 64
	var wg sync.WaitGroup
	bad := make(chan string, goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dir := dirs[i%len(dirs)]
			<-start
			got := gitRootFor(dir)
			if got != "" && got != dir+"-root" {
				bad <- got
			}
			// Interleave resets with lookups: a reset racing an in-flight probe
			// must not corrupt the memo or wedge a caller.
			if i%8 == 0 {
				ResetRepoRootCache()
			}
		}(i)
	}
	close(start)
	awaitGoroutines(t, &wg, "mixed concurrent lookups")

	close(bad)
	for got := range bad {
		t.Fatalf("lookup returned %q, which is neither a valid root nor the empty not-a-repo answer", got)
	}
	if got := inflightProbeCount(t); got != 0 {
		t.Fatalf("%d in-flight probe records left behind; want 0", got)
	}
}
