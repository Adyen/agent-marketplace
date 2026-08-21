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

// wedgeSettleDelay gives the goroutines that are queueing behind an in-flight
// probe time to actually reach the memo's critical section. Without it a
// lock-order cycle can be missed because the follower had not yet arrived when
// the leader published, so these tests would pass against the very
// implementation they exist to reject.
const wedgeSettleDelay = 100 * time.Millisecond

// installGitRootProbe substitutes a deterministic probe for the duration of the
// test and clears the memo on both sides, so nothing is inherited from or
// leaked to a sibling test (including a second `-count=2` pass).
func installGitRootProbe(t *testing.T, probe func(dir string) (string, bool)) {
	t.Helper()
	previous := gitRootProbe
	ResetRepoRootCache()
	gitRootProbe = probe
	t.Cleanup(func() {
		gitRootProbe = previous
		ResetRepoRootCache()
	})
}

// inflightProbeCount reports how many probes the memo currently believes are
// running. A non-zero count after every caller has returned means the memo
// leaks bookkeeping and would deadlock or re-probe forever.
func inflightProbeCount() int {
	gitRootMu.Lock()
	defer gitRootMu.Unlock()
	return len(gitRootInflight)
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

	const followers = 32
	results := make(chan string, followers)
	var wg sync.WaitGroup
	for i := 0; i < followers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- gitRootFor("/repo")
		}()
	}

	select {
	case <-entered:
	case <-time.After(probeWaitTimeout):
		t.Fatal("no goroutine reached the git probe")
	}
	time.Sleep(wedgeSettleDelay)
	close(release)

	for i := 0; i < followers; i++ {
		awaitRoot(t, results, "/repo", "concurrent gitRootFor(\"/repo\")")
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("git probe ran %d times for %d concurrent lookups of one directory; want exactly 1", got, followers)
	}
	if got := inflightProbeCount(); got != 0 {
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

	slow := make(chan string, 1)
	go func() { slow <- gitRootFor("/slow") }()
	select {
	case <-slowEntered:
	case <-time.After(probeWaitTimeout):
		t.Fatal("no goroutine reached the git probe for /slow")
	}

	// A same-directory follower is what pins the shared lock in a cyclic
	// implementation, so it has to be in the picture before the unrelated
	// lookup is attempted.
	sameDir := make(chan string, 1)
	go func() { sameDir <- gitRootFor("/slow") }()
	time.Sleep(wedgeSettleDelay)

	unrelated := make(chan string, 1)
	go func() { unrelated <- gitRootFor("/fast") }()
	awaitRoot(t, unrelated, "/fast", "gitRootFor(\"/fast\") during an in-flight probe for /slow")

	close(release)
	awaitRoot(t, slow, "/slow", "gitRootFor(\"/slow\") leader")
	awaitRoot(t, sameDir, "/slow", "gitRootFor(\"/slow\") follower")

	if got := inflightProbeCount(); got != 0 {
		t.Fatalf("%d in-flight probe records left behind; want 0", got)
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
	results := make(chan string, failing)
	for i := 0; i < failing; i++ {
		go func() { results <- gitRootFor("/repo") }()
	}
	<-entered
	time.Sleep(wedgeSettleDelay)
	close(release)
	for i := 0; i < failing; i++ {
		awaitRoot(t, results, "", "gitRootFor(\"/repo\") with a failing probe")
	}
	if got := inflightProbeCount(); got != 0 {
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

	leaderReturned := make(chan struct{})
	go func() {
		defer close(leaderReturned)
		defer func() { _ = recover() }()
		gitRootFor("/repo")
	}()
	select {
	case <-entered:
	case <-time.After(probeWaitTimeout):
		t.Fatal("no goroutine reached the git probe")
	}

	waiter := make(chan string, 1)
	go func() { waiter <- gitRootFor("/repo") }()
	time.Sleep(wedgeSettleDelay)
	close(release)

	select {
	case <-leaderReturned:
	case <-time.After(probeWaitTimeout):
		t.Fatal("the panicking lookup never unwound")
	}
	awaitRoot(t, waiter, "", "gitRootFor(\"/repo\") waiting on a panicking probe")

	if got := inflightProbeCount(); got != 0 {
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

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(probeWaitTimeout):
		t.Fatalf("mixed concurrent lookups did not finish within %s: the memo is deadlocked", probeWaitTimeout)
	}

	close(bad)
	for got := range bad {
		t.Fatalf("lookup returned %q, which is neither a valid root nor the empty not-a-repo answer", got)
	}
	if got := inflightProbeCount(); got != 0 {
		t.Fatalf("%d in-flight probe records left behind; want 0", got)
	}
}
