package materialize

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/testutil"
)

// This file is bead agent-marketplace-wrp8's (B3's) acceptance suite: the critical
// section in reconcileDevinAggregates now spans the CALLER's read of the aggregate
// and of the ownership record, not just each Apply.
//
// Three rules shape it.
//
// 1. CONTENTION IS CONSTRUCTED, NEVER TIMED. Every failure-mode test manufactures
// the contention with a REAL child process that has said, on a pipe, that it holds
// the lock. There is no sleep anywhere in this file and no "give it 100 ms".
//
// 2. THE CROSS-PROCESS TESTS USE REAL PROCESSES because an in-process test cannot
// distinguish flock from a package-level sync.Mutex, and the defect this bead
// closes is a two-terminal defect. The holder is a re-exec of this test binary; see
// TestDevinLockHolderChildProcess.
//
// 3. WHAT THE MATERIALIZE LEVEL CAN AND CANNOT SHOW. The lost update itself is
// asserted where it is observable — internal/ownership's
// TestAdversarialB2StillLosesAnUpdateWhenTheCallersReadIsOutsideTheSection and
// TestSessionsFromTwoProcessesNeitherLoseAnUpdate, which model exactly this
// caller's read-modify-commit shape and which go red when the session is removed.
// Two concurrent materialize.Run calls with DIFFERENT desired sets cannot exhibit
// it, because sync's own semantics make the second run prune the first run's
// now-undesired entry: "lost" and "correctly pruned" produce byte-identical files.
// So what is asserted here, over 200 trials, is the pair of properties that ARE
// observable through Run — the record keeps describing the file and contention
// produces no spurious write error — plus the per-root failure isolation, the
// single wait, and the lock-free dry run.

// ---------------------------------------------------------------------------
// A real child process that holds one root's ownership lock
// ---------------------------------------------------------------------------

const (
	devinHolderModeEnv   = "AM_DEVIN_LOCK_HOLDER"
	devinHolderRootEnv   = "AM_DEVIN_LOCK_HOLDER_TARGET_ROOT"
	devinHolderStateEnv  = "AM_DEVIN_LOCK_HOLDER_STATE_ROOT"
	devinHolderAcquired  = "ACQUIRED"
	devinHolderReleasing = "RELEASING"
)

// TestDevinLockHolderChildProcess is not a test: it is the body of the child
// process the tests below exec, inert unless its mode env var is set. It takes the
// per-root ownership lock, says so on stdout, and holds it until the parent closes
// its stdin.
//
// It exists so contention is produced by a SECOND PROCESS. A holder inside this
// process would share nothing with production reality: reconcileDevinAggregates
// opens its own Store per root, so the only thing that can exclude it is a kernel
// lock held by a different open file description.
func TestDevinLockHolderChildProcess(t *testing.T) {
	if os.Getenv(devinHolderModeEnv) == "" {
		t.Skip("not a child process invocation")
	}
	store, err := ownership.Open(os.Getenv(devinHolderRootEnv),
		ownership.WithStateRoot(os.Getenv(devinHolderStateEnv)))
	if err != nil {
		t.Fatalf("child Open: %v", err)
	}
	release, err := store.Lock()
	if err != nil {
		t.Fatalf("child Lock: %v", err)
	}
	if _, err := os.Stdout.WriteString(devinHolderAcquired + "\n"); err != nil {
		t.Fatalf("child announce: %v", err)
	}
	// Block on a pipe read rather than a timer: the parent decides when the
	// section ends, so the interleaving is constructed rather than hoped for.
	_, _ = os.Stdin.Read(make([]byte, 1))
	if _, err := os.Stdout.WriteString(devinHolderReleasing + "\n"); err != nil {
		t.Fatalf("child announce release: %v", err)
	}
	release()
}

// startDevinLockHolder execs a child that holds targetRoot's ownership lock and
// returns once the child has confirmed it. The returned function releases it.
func startDevinLockHolder(t *testing.T, targetRoot, stateRoot string) (letGo func()) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDevinLockHolderChildProcess$", "-test.timeout=120s")
	cmd.Env = append(os.Environ(),
		devinHolderModeEnv+"=1",
		devinHolderRootEnv+"="+targetRoot,
		devinHolderStateEnv+"="+stateRoot,
		// The holder must not inherit a short timeout from a test that set one:
		// it acquires an uncontended lock, but an inherited malformed or tiny
		// value would change what it is testing.
		ownership.LockTimeoutEnvVar+"=60s",
	)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("holder stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("holder stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	// A parked holder blocks on a pipe read, so a t.Fatal anywhere below would
	// otherwise leave it alive holding the lock until its own timeout expires.
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	scanner := bufio.NewScanner(stdout)
	if !scanFor(scanner, devinHolderAcquired) {
		t.Fatal("the holder child never reported acquiring the lock")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseHolder(t, cmd, stdin, scanner)
		})
	}
}

func releaseHolder(t *testing.T, cmd *exec.Cmd, stdin io.WriteCloser, scanner *bufio.Scanner) {
	t.Helper()
	_, _ = stdin.Write([]byte("\n"))
	_ = stdin.Close()
	if !scanFor(scanner, devinHolderReleasing) {
		t.Error("the holder child never reported releasing the lock")
	}
	if err := cmd.Wait(); err != nil {
		t.Errorf("holder child exited with an error: %v", err)
	}
}

func scanFor(scanner *bufio.Scanner, token string) bool {
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), token) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 1. Two concurrent runs on one repo
// ---------------------------------------------------------------------------

// TestDevinConcurrentRunsKeepTheRecordDescribingTheFile is the bead's 60/60 repro
// inverted, at the level the bead reported it: two concurrent materialize.Run calls
// against one repo, 200 times.
//
// Each Run opens its OWN ownership.Store for the root, so the two racers hold two
// open file descriptions and contend in the kernel exactly as two terminals would;
// nothing here would be satisfied by sharing a Store.
//
// Both racers want the SAME resources, which is the two-terminal case (one repo,
// one config, a save hook racing a human). What must hold in every trial:
//
//   - the durable record describes the bytes that are live, for BOTH aggregate
//     files of the root — the failure that made the parent bead a P0;
//   - every desired entry is present in the file and owned, so nothing became
//     permanently unmanaged;
//   - contention produced NO materialize.devin.aggregate-write error. That
//     diagnostic is the spurious ENOENT terminal failure the bead reported, and it
//     must be gone rather than rarer;
//   - no journal and no staged file survive.
func TestDevinConcurrentRunsKeepTheRecordDescribingTheFile(t *testing.T) {
	// 200 trials in a full run, 20 under -short. The race reproduced 60/60 before
	// the fix, so twenty trials is exactly as deterministic as two hundred; see
	// testutil.Trials.
	trials := testutil.Trials(testutil.FullTrials, testing.Short())
	// Set once, not per trial: t.Setenv registers a cleanup per call and a
	// 200-trial loop calling devinEnv would accumulate four hundred of them.
	// Each trial gets its own repo root, so one state/cache root is enough — the
	// ownership state is keyed by canonical root.
	_, _ = devinEnv(t)
	for trial := 0; trial < trials; trial++ {
		repoRoot := t.TempDir()
		root := devinProjectRoot(repoRoot)
		resolved := []resolver.ResolvedArtifact{
			devinResolved(t, "alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
			additiveSafetyProvider(t, hcc.TargetDevin, false, "echo beta", ""),
		}

		start := make(chan struct{})
		reports := make([]*Report, 2)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range reports {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				reports[i], errs[i] = Run(context.Background(), repoRoot, resolved)
			}(i)
		}
		close(start)
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("trial %d racer %d: Run: %v", trial, i, err)
			}
		}
		for i, report := range reports {
			for _, d := range report.Diags {
				if d.Code == diag.CodeMaterializeDevinAggregateWrite {
					t.Fatalf("trial %d racer %d: contention alone produced %s: %s.\n"+
						"That diagnostic is the spurious terminal failure this bead removes.",
						trial, i, d.Code, d.Message)
				}
				if d.Severity == diag.SeverityError {
					t.Fatalf("trial %d racer %d: unexpected error %s: %s", trial, i, d.Code, d.Message)
				}
			}
		}

		for _, rel := range []string{harness.DevinMcpConfigFile, harness.DevinProjectHooksFile} {
			assertRecordDescribesDevinFile(t, trial, root, rel)
		}
		if got := readFileString(t, filepath.Join(root, harness.DevinMcpConfigFile)); !strings.Contains(got, `"alpha"`) {
			t.Fatalf("trial %d: the mcp entry is missing after two concurrent runs:\n%s", trial, got)
		}
		if got := readFileString(t, filepath.Join(root, harness.DevinProjectHooksFile)); !strings.Contains(got, "echo beta") {
			t.Fatalf("trial %d: the hook entry is missing after two concurrent runs:\n%s", trial, got)
		}
	}
}

// assertRecordDescribesDevinFile is the invariant every writer must leave true: the
// durable ownership record describes the bytes that are live, nothing is left in
// flight, and no staged file survives.
func assertRecordDescribesDevinFile(t *testing.T, trial int, root, rel string) {
	t.Helper()
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatalf("trial %d: open store: %v", trial, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("trial %d: read %s: %v", trial, rel, err)
	}
	proof, err := store.Proof(rel)
	if err != nil {
		t.Fatalf("trial %d: proof for %s: %v", trial, rel, err)
	}
	if got, want := proof.ContentDigest(), ownership.Digest(raw); got != want {
		t.Fatalf("trial %d: the record for %s describes content %s but the live file is %s.\n"+
			"The next sync would classify a committed write as drift and rewrite it with a "+
			"misleading 'was edited by hand' warning.", trial, rel, got, want)
	}
	if proof.OwnedCount() == 0 {
		t.Fatalf("trial %d: the record claims no owned entries for %s, so its entries became unmanaged", trial, rel)
	}
	journal, err := store.ReadJournal()
	if err != nil {
		t.Fatalf("trial %d: read journal: %v", trial, err)
	}
	if journal != nil {
		t.Fatalf("trial %d: a completed run left an in-flight journal for %s", trial, journal.File)
	}
	staged, err := filepath.Glob(filepath.Join(root, rel) + ".am-staged-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 0 {
		t.Fatalf("trial %d: staged files survived: %v", trial, staged)
	}
}

// ---------------------------------------------------------------------------
// 2. A timeout skips exactly one root
// ---------------------------------------------------------------------------

// TestDevinLockTimeoutSkipsExactlyThatRoot pins per-root failure isolation, which
// is the same shape as "a malformed mcp_config.json blocks only that file".
//
// A real child process holds the PROJECT root's lock. The run must: report one
// actionable ownership.lock-timeout error naming that root, leave every one of that
// root's aggregates BYTE-IDENTICAL, still reconcile the machine-scope root (a
// different canonical root, therefore a different lock), and exit non-zero.
//
// The contention is manufactured, so there is no race and no schedule to be lucky
// about.
func TestDevinLockTimeoutSkipsExactlyThatRoot(t *testing.T) {
	repoRoot, home := devinEnv(t)
	t.Setenv(ownership.LockTimeoutEnvVar, "50ms")
	projectRoot := devinProjectRoot(repoRoot)
	userRoot := devinUserRoot(home)

	// A handwritten aggregate in the blocked root, so "left exactly as found" is
	// asserted against bytes a user would recognize.
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	handwritten := "{\n  // kept by hand\n  \"mcpServers\": {\n    \"mine\": {\"command\": \"mine\"}\n  }\n}\n"
	projectFile := filepath.Join(projectRoot, harness.DevinMcpConfigFile)
	if err := os.WriteFile(projectFile, []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}

	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	letGo := startDevinLockHolder(t, projectRoot, stateRoot)
	defer letGo()

	resolved := []resolver.ResolvedArtifact{
		devinResolved(t, "alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved(t, "machine", ir.NewMcpServer("machine", "machine-binary", nil, nil, nil, ir.SourceRef{}), true),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	timeouts := findDiags(report.Diags, diag.CodeOwnershipLockTimeout)
	if len(timeouts) != 1 {
		t.Fatalf("ownership.lock-timeout diagnostics = %d, want exactly 1: %v", len(timeouts), report.Diags)
	}
	if timeouts[0].Severity != diag.SeverityError {
		t.Fatalf("%s severity = %v, want error so the run exits non-zero", timeouts[0].Code, timeouts[0].Severity)
	}
	if !strings.Contains(timeouts[0].Message, projectRoot) {
		t.Fatalf("the timeout diagnostic does not name the skipped root %q: %s", projectRoot, timeouts[0].Message)
	}
	if diags := findDiags(report.Diags, diag.CodeMaterializeDevinAggregateWrite); len(diags) != 0 {
		t.Fatalf("a lock timeout was also reported as a write failure: %v", diags)
	}

	// The skipped root is byte-identical, comments and all.
	if got := readFileString(t, projectFile); got != handwritten {
		t.Fatalf("the skipped root's aggregate changed:\n--- want ---\n%s\n--- got ---\n%s", handwritten, got)
	}
	// And no ownership state was created for it: a refused acquisition writes
	// nothing, not even a safety backup.
	blocked, err := ownership.Open(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.EverRecorded() {
		t.Fatal("a skipped root was left with durable evidence that it was managed")
	}

	// The OTHER root is a different canonical root and was reconciled normally.
	if got := readFileString(t, filepath.Join(userRoot, harness.DevinMcpConfigFile)); !strings.Contains(got, `"machine"`) {
		t.Fatalf("one root's lock timeout blocked an unrelated root:\n%s", got)
	}

	// The run reports an error, which is what makes sync exit non-zero.
	hasError := false
	for _, d := range report.Diags {
		if d.Severity == diag.SeverityError {
			hasError = true
		}
	}
	if !hasError {
		t.Fatal("a skipped root must surface an error diagnostic so sync exits non-zero")
	}
}

// ---------------------------------------------------------------------------
// 3. One outer wait, not one per file
// ---------------------------------------------------------------------------

// TestDevinContendedRootWaitsOnceNotOncePerFile pins the USER-VISIBLE shape of a
// contended root: one wait, one report.
//
// # What it actually pins, corrected
//
// Its original docstring claimed the single ownership.lock-timeout diagnostic was
// itself the boundary assertion, because "a per-file section produces two". That
// claim is false, and this test passed with B3's outer session removed
// (agent-marketplace-npwr measured it). The reason is Recover: Store.Recover
// acquires unconditionally, even when the journal is empty, and it runs BEFORE the
// per-file loop — so in the per-file counterfactual Recover is the first thing to
// time out, reconcileDevinAggregates takes its recoverErr early return, and the
// per-file waits never happen at all. One diagnostic either way. Pre-seeding a
// completed generation does not change that, because a no-op Recover still
// acquires.
//
// So the corrected claim is the narrower one this test can actually make, and it
// is still worth making: a contended root produces EXACTLY ONE actionable timeout
// report (not one per aggregate file, and not one per mutation inside the
// section), and it spends less than two timeouts of wall clock doing it. Both
// halves are about what a user sees and how long they wait for it.
//
// The SECTION BOUNDARY is pinned by TestDevinRunAcquiresOncePerRootNotOncePerFile,
// which counts acquisitions and is the test that goes red when the session is
// removed. This one deliberately does not duplicate that claim.
func TestDevinContendedRootWaitsOnceNotOncePerFile(t *testing.T) {
	const timeout = 500 * time.Millisecond
	repoRoot, _ := devinEnv(t)
	t.Setenv(ownership.LockTimeoutEnvVar, timeout.String())
	projectRoot := devinProjectRoot(repoRoot)

	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	letGo := startDevinLockHolder(t, projectRoot, stateRoot)
	defer letGo()

	// Work for BOTH aggregate files of the root, so a per-file section would have
	// two independent waits to spend.
	resolved := []resolver.ResolvedArtifact{
		devinResolved(t, "alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
		additiveSafetyProvider(t, hcc.TargetDevin, false, "echo beta", ""),
	}

	started := time.Now()
	report, err := Run(context.Background(), repoRoot, resolved)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := len(findDiags(report.Diags, diag.CodeOwnershipLockTimeout)); got != 1 {
		t.Fatalf("ownership.lock-timeout diagnostics = %d, want exactly 1 for one contended root;"+
			" a user must be told once that another process holds the lock, not once per aggregate"+
			" file and not once per mutation: %v", got, report.Diags)
	}
	// Two aggregate files, so a per-file section would spend at least 2 timeouts.
	// The bound is deliberately loose: it must separate 1 wait from N waits, not
	// measure scheduler noise.
	if elapsed >= 3*timeout {
		t.Fatalf("a contended root waited %s, i.e. more than one timeout of %s;"+
			" the whole point of one outer section is one wait", elapsed, timeout)
	}
}

// TestDevinRunAcquiresOncePerRootNotOncePerFile is the section BOUNDARY test, and
// the one that goes red the moment the outer session is removed.
//
// It is here because no other assertion can see the boundary. On an uncontended
// root a per-root session and a per-file one write the same bytes and produce the
// same diagnostics; they differ only in how many times the lock is taken. And that
// count is the whole of the per-file timeout multiplication: N acquisitions on a
// contended root are N waits, so a root with two Devin aggregates could burn 2× the
// timeout — and 2N in general — before reporting anything.
//
// The expected shape for a root with both aggregate files is ONE acquisition and
// three reentries: Recover, then one Apply per file, each bumping the depth of the
// session the reconciler already holds. Without the session it is three
// acquisitions and no reentries.
func TestDevinRunAcquiresOncePerRootNotOncePerFile(t *testing.T) {
	repoRoot, _ := devinEnv(t)

	var mu sync.Mutex
	var acquired, reentered int
	var maxDepth int
	ownedAggregateLockAcquisitionHook = func(ev ownership.LockEvent) {
		mu.Lock()
		defer mu.Unlock()
		switch ev.Kind {
		case ownership.LockAcquired:
			acquired++
		case ownership.LockReentered:
			reentered++
		}
		if ev.Depth > maxDepth {
			maxDepth = ev.Depth
		}
	}
	t.Cleanup(func() { ownedAggregateLockAcquisitionHook = nil })

	resolved := []resolver.ResolvedArtifact{
		devinResolved(t, "alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
		additiveSafetyProvider(t, hcc.TargetDevin, false, "echo beta", ""),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)

	mu.Lock()
	defer mu.Unlock()
	if acquired != 1 {
		t.Fatalf("one root's reconciliation took the lock %d times, want exactly 1.\n"+
			"Each acquisition is an independent wait on a contended root, so N of them means a "+
			"contended root can burn N timeouts before reporting anything — and it means the "+
			"caller's read of the aggregate is outside the section that protects its commit.", acquired)
	}
	if reentered != 3 {
		t.Fatalf("inner acquisitions reentered %d times, want 3 (Recover plus one Apply per aggregate file);"+
			" a different number means the loop is no longer inside one session", reentered)
	}
	if maxDepth != 2 {
		t.Fatalf("maximum lock depth = %d, want 2: the session plus one sequential mutation at a time", maxDepth)
	}
}

func TestOwnedAggregateWriteSessionRegistrationExpiresAfterCallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	root := t.TempDir()
	canonical, err := ownership.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(canonical)
	if err != nil {
		t.Fatal(err)
	}
	session := ownedAggregateWriteSession{root: canonical, store: store, lockReport: newOwnedAggregateLockReporter(outputKey{target: hcc.TargetDevin}, canonical)}
	callbackFailure := errors.New("adapter failed after session entry")
	for _, tc := range []struct {
		name string
		err  error
	}{{name: "complete"}, {name: "callback error", err: callbackFailure}} {
		t.Run(tc.name, func(t *testing.T) {
			state := ownedAggregateState{}
			err := withOwnedAggregateWriteSession(context.Background(), &state, session, "test-shared-root-session", func() error {
				registered, ok := state.writeSession(root)
				if !ok || registered != session {
					return fmt.Errorf("session lookup inside callback = %#v, %v", registered, ok)
				}
				return tc.err
			})
			if tc.err == nil && err != nil || tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("session callback error = %v, want %v", err, tc.err)
			}
			if registered, ok := state.writeSession(root); ok {
				t.Fatalf("session survived callback completion: %#v", registered)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. The dry-run path is lock-free
// ---------------------------------------------------------------------------

// TestDevinDryRunNeitherLocksNorBlocks is the invariant `sync --check`, `status`,
// `list`, the TUI background refresh and `doctor`'s preview all depend on. They all
// reach the Devin reconciler only through WithDryRun, so this is the shared
// mechanism rather than a per-command claim.
//
// Two things must hold, and the second is the one a widened section could most
// easily break: a dry run must create NOTHING under $XDG_STATE_HOME (acquisition is
// what creates locks/, and Open's contract is to be side-effect free), and it must
// not block behind a writer that holds the lock — checked with a real holder and
// the DEFAULT 30 s timeout, so a dry run that acquired would be unmissable rather
// than flaky.
func TestDevinDryRunNeitherLocksNorBlocks(t *testing.T) {
	t.Run("creates nothing under the state root", func(t *testing.T) {
		repoRoot, _ := devinEnv(t)
		stateRoot, err := ownership.StateRoot()
		if err != nil {
			t.Fatal(err)
		}
		resolved := []resolver.ResolvedArtifact{
			devinResolved(t, "alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
		}
		if _, err := Run(context.Background(), repoRoot, resolved, WithDryRun()); err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if _, err := os.Lstat(stateRoot); !os.IsNotExist(err) {
			t.Fatalf("a dry run created the state root %s (%v); status and sync --check must touch nothing", stateRoot, err)
		}
	})

	t.Run("does not block on a held lock", func(t *testing.T) {
		repoRoot, _ := devinEnv(t)
		projectRoot := devinProjectRoot(repoRoot)
		stateRoot, err := ownership.StateRoot()
		if err != nil {
			t.Fatal(err)
		}
		// Seed a real generation so the dry run has a record to read, which is the
		// realistic shape of `status` on a synced repo.
		resolved := []resolver.ResolvedArtifact{
			devinResolved(t, "alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
		}
		if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
			t.Fatalf("seed run: %v", err)
		}

		letGo := startDevinLockHolder(t, projectRoot, stateRoot)
		defer letGo()

		before := stateRootSnapshot(t, stateRoot)
		// The DEFAULT timeout: blocking would take 30s and then fail.
		deadline := time.Now().Add(10 * time.Second)
		report, err := Run(context.Background(), repoRoot,
			append(resolved, devinResolved(t, "beta", ir.NewHook("beta", "PostToolUse", "echo beta", "command", nil, ir.SourceRef{}), false)),
			WithDryRun())
		if err != nil {
			t.Fatalf("dry run while the lock was held: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the dry run blocked; it is waiting on the per-root ownership lock")
		}
		for _, d := range report.Diags {
			if d.Code == diag.CodeOwnershipLockTimeout || d.Code == diag.CodeOwnershipLockContended {
				t.Fatalf("a dry run reported a locking diagnostic %s: %s", d.Code, d.Message)
			}
		}
		// It saw the drift it was asked about, and changed nothing.
		if after := stateRootSnapshot(t, stateRoot); after != before {
			t.Fatalf("a dry run changed the state root:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})
}

// stateRootSnapshot renders every path under the state root with its size, so a
// test can assert a read-only pass changed nothing at all.
func stateRootSnapshot(t *testing.T, root string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			fmt.Fprintf(&sb, "%s dir\n", rel)
			return nil
		}
		fmt.Fprintf(&sb, "%s %d\n", rel, info.Size())
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// 5. Contention and degradation reach the report
// ---------------------------------------------------------------------------

// TestDevinLockReporterSeverities pins the contention diagnostics' shape without a
// sleep: the reporter is fed the lock events it would observe.
//
// A wait is Info, because a save hook racing a human is the mechanism working and
// must not inflate the warning count --strict keys off; a sub-threshold wait is not
// reported at all, because a few milliseconds is not actionable; a degradation is a
// Warning, and exactly one per root however many mutations the session contained.
func TestDevinLockReporterSeverities(t *testing.T) {
	key := outputKey{target: hcc.TargetDevin, machineScope: false}
	root := filepath.Join(t.TempDir(), ".devin")

	t.Run("a long wait is one Info diagnostic naming the root", func(t *testing.T) {
		r := newOwnedAggregateLockReporter(key, root)
		r.observe(ownership.LockEvent{Kind: ownership.LockContended, Attempts: 1})
		r.observe(ownership.LockEvent{Kind: ownership.LockAcquired, Waited: 2 * time.Second, Attempts: 9})
		r.observe(ownership.LockEvent{Kind: ownership.LockReentered, Depth: 2})
		r.observe(ownership.LockEvent{Kind: ownership.LockReentered, Depth: 2})

		diags := r.diags()
		if len(diags) != 1 || diags[0].Code != diag.CodeOwnershipLockContended {
			t.Fatalf("diags = %+v, want one %s", diags, diag.CodeOwnershipLockContended)
		}
		if diags[0].Severity != diag.SeverityInfo {
			t.Fatalf("%s severity = %v, want info", diags[0].Code, diags[0].Severity)
		}
		if !strings.Contains(diags[0].Message, root) {
			t.Fatalf("the contention diagnostic does not name the root: %s", diags[0].Message)
		}
	})

	t.Run("a few milliseconds of contention is not reported", func(t *testing.T) {
		r := newOwnedAggregateLockReporter(key, root)
		r.observe(ownership.LockEvent{Kind: ownership.LockContended, Attempts: 1})
		r.observe(ownership.LockEvent{Kind: ownership.LockAcquired, Waited: 3 * time.Millisecond, Attempts: 2})
		if diags := r.diags(); len(diags) != 0 {
			t.Fatalf("diags = %+v, want none for a sub-second wait", diags)
		}
	})

	t.Run("an uncontended acquisition reports nothing", func(t *testing.T) {
		r := newOwnedAggregateLockReporter(key, root)
		r.observe(ownership.LockEvent{Kind: ownership.LockAcquired, Attempts: 1})
		r.observe(ownership.LockEvent{Kind: ownership.LockReleased})
		if diags := r.diags(); len(diags) != 0 {
			t.Fatalf("diags = %+v, want none", diags)
		}
	})

	t.Run("a degraded root warns once, not once per mutation", func(t *testing.T) {
		r := newOwnedAggregateLockReporter(key, root)
		for i := 0; i < 4; i++ {
			r.observe(ownership.LockEvent{Kind: ownership.LockUnsupported, Attempts: 1})
		}
		diags := r.diags()
		if len(diags) != 1 || diags[0].Code != diag.CodeOwnershipLockUnavailable {
			t.Fatalf("diags = %+v, want exactly one %s", diags, diag.CodeOwnershipLockUnavailable)
		}
		if diags[0].Severity != diag.SeverityWarning {
			t.Fatalf("%s severity = %v, want warning", diags[0].Code, diags[0].Severity)
		}
	})
}

// TestDevinBackupOnlyFailureKeepsTheCodedErrorsOwnCode is the mislabelled-code
// item.
//
// The malformed-aggregate branch takes a mandatory safety backup before reporting
// the parse error, and it used to label ANY failure there ownership.safety-backup —
// so a lock timeout was reported as "the mandatory raw-byte safety backup could not
// be written", pointing a user at state-directory permissions for a problem that is
// another process holding the lock. The coded error's own code now wins, exactly as
// the Recover and Apply sites already did it.
//
// The contention is real: a child process holds the lock, and the aggregate is
// malformed, so BackupOnly is the call that fails.
func TestDevinBackupOnlyFailureKeepsTheCodedErrorsOwnCode(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	t.Setenv(ownership.LockTimeoutEnvVar, "50ms")
	projectRoot := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	malformed := "{ this is not json"
	abs := filepath.Join(projectRoot, harness.DevinMcpConfigFile)
	if err := os.WriteFile(abs, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	letGo := startDevinLockHolder(t, projectRoot, stateRoot)
	defer letGo()

	// The reconciler is called directly, because the whole point is the diagnostic
	// produced INSIDE the session by a mutation that fails while the section is
	// held elsewhere. Run's own outer session would refuse before reaching it.
	key := outputKey{target: hcc.TargetDevin, machineScope: false}
	out := reconcileDevinAggregateFile(store, key, projectRoot, harness.DevinMcpConfigFile,
		[]ownedJSONEntry{}, false, false)

	var backupCoded, malformedSeen bool
	for _, d := range out.diags {
		switch {
		case d.Code == diag.CodeOwnershipLockTimeout:
			backupCoded = true
		case d.Code == diag.CodeOwnershipSafetyBackup:
			t.Fatalf("a lock timeout on the safety-backup path was mislabelled %s: %s", d.Code, d.Message)
		case d.Code == diag.CodeMaterializeDevinAggregateMalformed || strings.HasPrefix(d.Code, "json."):
			// The parse failure keeps the parser's own code (json.malformed) when
			// it has one, which is the pre-existing behaviour of this branch and is
			// unrelated to the backup label being fixed.
			malformedSeen = true
		}
	}
	if !backupCoded {
		t.Fatalf("the coded lock timeout did not reach the report: %+v", out.diags)
	}
	if !malformedSeen {
		t.Fatalf("the malformed aggregate was not reported: %+v", out.diags)
	}
	// And the malformed file is still exactly the user's bytes.
	if got := readFileString(t, abs); got != malformed {
		t.Fatalf("a malformed aggregate was rewritten: %q", got)
	}
}
