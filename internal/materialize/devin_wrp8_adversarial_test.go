package materialize

import (
	"bufio"
	"context"
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

// This file is the ADVERSARIAL suite for bead agent-marketplace-wrp8 (B3): the
// critical section widened over reconcileDevinAggregates.
//
// It exists because of a measurement, not a hunch. Replacing
// [ownership.Store.WithWriteSessionContext] with a pass-through — i.e. deleting
// the whole of B3 while leaving B1 and B2 in place — turned exactly ONE test in
// this package red, TestDevinRunAcquiresOncePerRootNotOncePerFile, and that test
// counts lock ACQUISITIONS. Not one test asserted a behavioural consequence:
// the 200-trial TestDevinConcurrentRunsKeepTheRecordDescribingTheFile passed
// with the section removed, and so did the timeout, single-wait and dry-run
// tests. A counter is a fine boundary pin, but it is not evidence that a P0 was
// closed, and it is the kind of assertion a future refactor deletes as
// "implementation detail" without noticing what went with it.
//
// The tests below are the missing behavioural half. Two of them are load-bearing:
// they go red on a tree where the widened section is removed, and they go red for
// the reason the parent bead describes — an entry silently lost because a writer's
// patch was computed from bytes another writer had already replaced.
//
// # What the residual window actually is, and why one of these tests widens it
//
// Worth stating precisely, because it bounds how strong any repro here can be.
// With B2 alone the write path is already bracketed twice: Recover acquires and
// releases around itself, and Apply acquires and releases around the whole
// generation. What is left UNPROTECTED is the gap between those two — Recover's
// release to Apply's acquisition — and that gap is exactly where the caller's
// os.ReadFile, jsonc parse, store.Proof, Classify and patch live. It is a few
// microseconds wide.
//
// Sampling a few-microsecond window is the flaky repro the design document
// forbids: the trial loop below measured 11/100 lost on one execution of a
// session-less tree and 1/100 on another. So there are two shapes here on
// purpose. The trial loop reports a RATE, which is what makes the number in this
// bead's report reproducible. The other one holds the window OPEN with a rule
// that is provably a no-op on a correct tree, and is therefore deterministic: it
// is the one to keep if only one can be kept.
//
// # Why the existing 200-trial test cannot show a loss, and this one can
//
// Two runs with different desired sets normally produce byte-identical files
// whether the second one LOST the first one's entry or correctly PRUNED it as a
// stale owned entry — the two outcomes are the same bytes. Every test here
// therefore makes both racers PARTIAL runs, using a rule the plan already states
// and ownedJSONPatchPlan already implements: "A partial run does not prune". With
// pruning off, the union is the only correct end state and a missing entry can
// only be a lost update.

// ---------------------------------------------------------------------------
// 1. The lost update, made observable through the production reconciler
// ---------------------------------------------------------------------------

// devinMcpDesired builds one desired mcp_config.json entry with canonical value
// bytes, so its digest is the same one the reconciler derives from the file.
func devinMcpDesired(t *testing.T, key, command string) ownedJSONEntry {
	t.Helper()
	raw, err := canonicalOwnedJSON([]byte(`{"command":"` + command + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	return ownedJSONEntry{
		rel:       harness.DevinMcpConfigFile,
		kind:      ownership.KindMcpServer,
		container: devinMcpServersKey,
		key:       key,
		raw:       raw,
		identity:  ownership.McpIdentityDigest(key),
		resource:  "resource-" + key,
	}
}

func newDevinReport() *Report {
	return &Report{PerTarget: make(map[hcc.Target]TargetReport)}
}

// TestAdversarialTwoPartialRunsMustNotLoseAnEntry is the behavioural repro the
// suite was missing: a lost update through the production reconciler, at the
// materialize level, that a pass-through session actually loses.
//
// The existing 200-trial test cannot show a loss and says so: two runs with
// different desired sets produce byte-identical files whether the second one
// LOST the first one's entry or correctly PRUNED it, because a stale owned entry
// is exactly what sync removes. So the two outcomes are indistinguishable — as
// long as pruning is on.
//
// The construction here turns pruning off, using a rule the plan already states
// and ownedJSONPatchPlan already implements: "A partial run does not prune" — when
// `incomplete` is set for a file, the prune list is emptied. Both racers are
// partial runs against the same aggregate, so NEITHER of them is allowed to
// remove the other's entry for any reason. The union is therefore the only
// correct end state, and "lost" and "pruned" stop being the same bytes:
//
//   - With the widened section, the second racer's READ happens after the first
//     racer's commit is published. It sees the first entry, is forbidden to
//     prune it, and writes the union.
//   - Without it, both racers read the same empty document and the second one's
//     patch — computed from bytes the first had already replaced — drops the
//     first entry. Every entry Apply's own lock protects is still committed
//     consistently; the entry is simply gone.
//
// Severity if this fails: STATE CORRUPTION / LOST UPDATE, and the exact one the
// parent bead reports. The entry vanishes from a user's mcp_config.json while
// both runs exit 0.
func TestAdversarialTwoPartialRunsMustNotLoseAnEntry(t *testing.T) {
	// Halved from the 200-trial default because each trial here is two full
	// reconciliations, and scaled again under -short. See testutil.Trials.
	trials := testutil.Trials(testutil.FullTrials/2, testing.Short())
	// Set once, not per trial: t.Setenv registers a cleanup per call and a
	// 100-trial loop calling devinEnv would accumulate four hundred of them.
	// Each trial gets its own canonical root, so one state root is enough — the
	// rootKey differs per trial.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	lost := 0
	for trial := 0; trial < trials; trial++ {
		root := devinTrialRoot(t)
		key := outputKey{target: hcc.TargetDevin, machineScope: false}
		rel := harness.DevinMcpConfigFile

		// Build desired entries on the main test goroutine; devinMcpDesired uses
		// t.Fatal, which only exits the calling goroutine and must not be called
		// from a racer goroutine.
		entries := []ownedJSONEntry{
			devinMcpDesired(t, "alpha", "alpha-binary"),
			devinMcpDesired(t, "beta", "beta-binary"),
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, e := range entries {
			wg.Add(1)
			go func(entry ownedJSONEntry) {
				defer wg.Done()
				work := &ownedJSONAggregateWork{}
				work.add(entry)
				// A partial run: this racer had a resource skipped for this
				// file, so it is forbidden to prune anything from it.
				work.markIncomplete(rel)
				<-start
				reconcileDevinAggregates(context.Background(), newDevinReport(), key, root, work, false)
			}(e)
		}
		close(start)
		wg.Wait()

		got := readFileString(t, filepath.Join(root, rel))
		missing := []string{}
		for _, name := range []string{"alpha", "beta"} {
			if !strings.Contains(got, `"`+name+`"`) {
				missing = append(missing, name)
			}
		}
		if len(missing) != 0 {
			lost++
			t.Errorf("trial %d: entries %v were lost from %s although NEITHER run was allowed to prune:\n%s\n"+
				"A racer's read of the aggregate escaped the critical section, so its patch was computed "+
				"from bytes the other racer had already replaced. This is the lost update bead "+
				"agent-marketplace-wrp8 exists to remove.", trial, missing, rel, got)
		}
	}
	t.Logf("LOST-UPDATE RATE through reconcileDevinAggregates with the widened session: %d/%d trials lost an entry", lost, trials)
}

// TestAdversarialTheReadIsInsideTheSectionOrTheEntryIsLost is the same defect as
// the trial loop above with the window held OPEN instead of sampled, so that one
// trial is enough.
//
// The trial loop is honest but weak: measured against a tree with the session
// replaced by a pass-through it lost an entry in 11/100 runs on one execution and
// 1/100 on another. That is because the residual window B3 closes is genuinely
// narrow — Recover's own acquisition already brackets everything before the read,
// and Apply's brackets the commit, so what is left is the few microseconds
// between Recover's RELEASE and Apply's ACQUISITION, which is where the caller's
// read and jsonc patch live. A repro that hits a few-microsecond window a few
// times in a hundred is exactly the flaky repro the design document forbids.
//
// So the window is widened by a rule that is a NO-OP on a correct tree: sleep
// once, at the first lock ACQUISITION that is preceded by a RELEASE.
//
//   - On a correct tree a reconciliation takes exactly ONE acquisition and emits
//     it before any of its own releases, so the only acquisition that can follow a
//     release is the SECOND racer's session — which is already excluded and
//     already reads post-commit bytes. The sleep delays a start; it cannot create
//     a window, and no entry can be lost.
//   - Without the session, the first acquisition that follows a release is a
//     racer's Apply — i.e. after that racer has read and patched. Sleeping there,
//     holding the lock, is precisely the parent bead's interleaving: the other
//     racer reads the not-yet-replaced bytes (reading takes no lock and must not),
//     blocks on the commit, and then publishes a patch computed from bytes that
//     were replaced while it waited.
//
// Both racers are partial runs, so neither is permitted to prune and "lost" can no
// longer be mistaken for "correctly pruned".
func TestAdversarialTheReadIsInsideTheSectionOrTheEntryIsLost(t *testing.T) {
	const widen = 500 * time.Millisecond
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := harness.DevinMcpConfigFile
	key := outputKey{target: hcc.TargetDevin, machineScope: false}

	var mu sync.Mutex
	released := false
	var once sync.Once
	ownedAggregateLockAcquisitionHook = func(ev ownership.LockEvent) {
		switch ev.Kind {
		case ownership.LockReleased:
			mu.Lock()
			released = true
			mu.Unlock()
			return
		case ownership.LockAcquired:
		default:
			return
		}
		mu.Lock()
		afterARelease := released
		mu.Unlock()
		if !afterARelease {
			return
		}
		once.Do(func() { time.Sleep(widen) })
	}
	t.Cleanup(func() { ownedAggregateLockAcquisitionHook = nil })

	// Build desired entries on the main test goroutine; devinMcpDesired uses
	// t.Fatal, which only exits the calling goroutine and must not be called
	// from a racer goroutine.
	entries := []ownedJSONEntry{
		devinMcpDesired(t, "alpha", "alpha-binary"),
		devinMcpDesired(t, "beta", "beta-binary"),
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(entry ownedJSONEntry) {
			defer wg.Done()
			work := &ownedJSONAggregateWork{}
			work.add(entry)
			work.markIncomplete(rel)
			<-start
			reconcileDevinAggregates(context.Background(), newDevinReport(), key, root, work, false)
		}(e)
	}
	close(start)
	wg.Wait()

	got := readFileString(t, filepath.Join(root, rel))
	for _, name := range []string{"alpha", "beta"} {
		if !strings.Contains(got, `"`+name+`"`) {
			t.Fatalf("%q was lost from %s although NEITHER run was allowed to prune:\n%s\n"+
				"An acquisition happened AFTER a release within one reconciliation, which means the "+
				"caller's read of the aggregate is outside the section that protects its commit: the "+
				"other racer's patch was computed from bytes this one had already replaced.", name, rel, got)
		}
	}
	// And B2's invariant still holds on top of it: the record describes the live
	// bytes, so the next sync sees owned-clean rather than invented drift.
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(rel)
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	if got, want := proof.ContentDigest(), ownership.Digest(raw); got != want {
		t.Fatalf("the record describes %s but the live file is %s", got, want)
	}
}

// devinTrialRoot is devinEnv for a test that runs many trials in one function:
// t.Setenv registers a cleanup per call, so a 100-trial loop calling devinEnv
// would accumulate 400 of them. The env vars are set once and only the roots
// change.
func devinTrialRoot(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, ".devin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestAdversarialTwoPartialRunsAcrossTwoProcessesMustNotLoseAnEntry is the same
// property across two REAL processes, which is the case that matters and the one
// an in-process mutex cannot fake.
//
// It is deliberately separate from the in-process trial loop above: that one
// measures a rate cheaply, this one proves the mechanism is a kernel lock. B6
// owns the full multi-process suite; this is the one multi-process assertion
// that belongs to B3 because it is B3's own defect.
func TestAdversarialTwoPartialRunsAcrossTwoProcessesMustNotLoseAnEntry(t *testing.T) {
	if os.Getenv(devinLostUpdateChildEnv) != "" {
		t.Skip("this is the child-process body")
	}
	// Each trial forks two processes, so this loop is an order of magnitude more
	// expensive per trial than the in-process one above; -short scales it to two.
	trials := testutil.Trials(testutil.FullTrials/8, testing.Short())
	lost := 0
	skipped := 0
	for trial := 0; trial < trials; trial++ {
		repoRoot, _ := devinEnv(t)
		root := devinProjectRoot(repoRoot)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		// The XDG root, not ownership.StateRoot(): StateRoot() appends
		// "agent-marketplace" itself, so passing it back through
		// XDG_STATE_HOME would nest it twice and give the children a state
		// root the parent never looks at.
		if !runDevinLostUpdateChildren(t, root, os.Getenv("XDG_STATE_HOME")) {
			skipped++
			continue
		}
		got := readFileString(t, filepath.Join(root, harness.DevinMcpConfigFile))
		for _, name := range []string{"alpha", "beta"} {
			if !strings.Contains(got, `"`+name+`"`) {
				lost++
				t.Errorf("trial %d: %q was lost across two PROCESSES although neither was allowed to prune:\n%s",
					trial, name, got)
				break
			}
		}
	}
	t.Logf("CROSS-PROCESS LOST-UPDATE RATE: %d/%d trials lost an entry (%d skipped)", lost, trials, skipped)
	if skipped == trials {
		t.Fatalf("all %d trials were skipped due to child-process infrastructure issues; the test proved nothing", trials)
	}
}

const (
	devinLostUpdateChildEnv   = "AM_DEVIN_LOST_UPDATE_CHILD"
	devinLostUpdateRootEnv    = "AM_DEVIN_LOST_UPDATE_TARGET_ROOT"
	devinLostUpdateStateEnv   = "AM_DEVIN_LOST_UPDATE_STATE_ROOT"
	devinLostUpdateReadyToken = "READY"
	devinLostUpdateDoneToken  = "DONE"
)

// TestDevinLostUpdateChildProcess is not a test: it is the body of the child the
// cross-process repro execs, inert unless its mode env var is set. It performs
// exactly ONE partial-run reconciliation of the given root, announcing readiness
// first so the parent can release both children together — a constructed
// rendezvous, not a sleep.
func TestDevinLostUpdateChildProcess(t *testing.T) {
	name := os.Getenv(devinLostUpdateChildEnv)
	if name == "" {
		t.Skip("not a child process invocation")
	}
	root := os.Getenv(devinLostUpdateRootEnv)
	t.Setenv("XDG_STATE_HOME", os.Getenv(devinLostUpdateStateEnv))

	work := &ownedJSONAggregateWork{}
	work.add(devinMcpDesired(t, name, name+"-binary"))
	work.markIncomplete(harness.DevinMcpConfigFile)

	if _, err := os.Stdout.WriteString(devinLostUpdateReadyToken + "\n"); err != nil {
		t.Fatalf("child announce: %v", err)
	}
	_, _ = os.Stdin.Read(make([]byte, 1))

	report := reconcileDevinAggregates(context.Background(), newDevinReport(),
		outputKey{target: hcc.TargetDevin, machineScope: false}, root, work, false)
	for _, d := range report.Diags {
		if d.Severity == diag.SeverityError {
			t.Fatalf("child %s: %s: %s", name, d.Code, d.Message)
		}
	}
	if _, err := os.Stdout.WriteString(devinLostUpdateDoneToken + "\n"); err != nil {
		t.Fatalf("child announce done: %v", err)
	}
}

// runDevinLostUpdateChildren starts two children, waits until BOTH have said they
// are ready, releases them together, and waits for both to finish. It reports
// false when a child failed for a reason unrelated to the property under test, so
// the caller skips that trial rather than reporting a false positive.
func runDevinLostUpdateChildren(t *testing.T, root, xdgStateHome string) bool {
	t.Helper()
	type child struct {
		cmd     *exec.Cmd
		stdin   io.WriteCloser
		scanner *bufio.Scanner
	}
	var children []*child
	for _, name := range []string{"alpha", "beta"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDevinLostUpdateChildProcess$", "-test.timeout=120s")
		cmd.Env = append(os.Environ(),
			devinLostUpdateChildEnv+"="+name,
			devinLostUpdateRootEnv+"="+root,
			devinLostUpdateStateEnv+"="+xdgStateHome,
			ownership.LockTimeoutEnvVar+"=60s",
		)
		cmd.Stderr = os.Stderr
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		c := &child{cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout)}
		children = append(children, c)
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	for _, c := range children {
		if !scanFor(c.scanner, devinLostUpdateReadyToken) {
			t.Fatal("a child never announced readiness")
		}
	}
	// Released together: both are parked on a pipe read with their desired set
	// already built, so the only thing left in each of them is the section.
	for _, c := range children {
		_, _ = c.stdin.Write([]byte("\n"))
		_ = c.stdin.Close()
	}
	ok := true
	for _, c := range children {
		if !scanFor(c.scanner, devinLostUpdateDoneToken) {
			ok = false
		}
		if err := c.cmd.Wait(); err != nil {
			ok = false
		}
	}
	if !ok {
		t.Logf("a child reconciliation failed; skipping this trial, it proves nothing either way")
	}
	return ok
}

// ---------------------------------------------------------------------------
// 2. The env var as a de-serialization lever
// ---------------------------------------------------------------------------

// TestAdversarialZeroLockTimeoutFailsLoudlyRatherThanProceedingUnserialized is
// the highest-risk configuration attack: AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT=0
// is the one value a CI harness or an impatient user is most likely to set, and
// it is documented as "fail fast".
//
// "Fail fast" must mean REFUSE THE ROOT, not "skip the lock and write anyway".
// A value that turned serialization off while still mutating would silently
// reinstate the P0 on every machine that set it, and it would do so with a green
// exit code.
func TestAdversarialZeroLockTimeoutFailsLoudlyRatherThanProceedingUnserialized(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	t.Setenv(ownership.LockTimeoutEnvVar, "0")
	projectRoot := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	handwritten := "{\n  // kept by hand\n  \"mcpServers\": {\n    \"mine\": {\"command\": \"mine\"}\n  }\n}\n"
	abs := filepath.Join(projectRoot, harness.DevinMcpConfigFile)
	if err := os.WriteFile(abs, []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}

	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	letGo := startDevinLockHolder(t, projectRoot, stateRoot)
	defer letGo()

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{
		devinResolved("alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := len(findDiags(report.Diags, diag.CodeOwnershipLockTimeout)); got != 1 {
		t.Fatalf("with a zero timeout on a contended root, ownership.lock-timeout diagnostics = %d, want exactly 1: %v",
			got, report.Diags)
	}
	// The whole point: nothing was written, so a zero timeout cannot be a way to
	// run unserialized.
	if got := readFileString(t, abs); got != handwritten {
		t.Fatalf("a zero lock timeout WROTE to a contended root instead of refusing it:\n--- want ---\n%s\n--- got ---\n%s",
			handwritten, got)
	}
	blocked, err := ownership.Open(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.EverRecorded() {
		t.Fatal("a zero-timeout refusal left durable evidence that this root was managed")
	}
	if !strings.Contains(strings.Join(diagCodes(report.Diags), " "), diag.CodeOwnershipLockTimeout) {
		t.Fatalf("the refusal is not reported: %v", report.Diags)
	}
}

func diagCodes(diags []diag.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

// ---------------------------------------------------------------------------
// 3. Two contended roots, and the machine-scope direction
// ---------------------------------------------------------------------------

// TestAdversarialBothRootsContendedSkipBothAndReportEachOnce attacks the
// per-root failure-isolation claim from the direction the existing test does not
// cover: BOTH roots blocked at once, and the machine-scope root blocked while
// the repo root succeeds (the existing test only does the reverse).
//
// The contract is one diagnostic per skipped root, both aggregates byte-
// identical, and no cross-contamination of the two roots' state.
func TestAdversarialBothRootsContendedSkipBothAndReportEachOnce(t *testing.T) {
	repoRoot, home := devinEnv(t)
	t.Setenv(ownership.LockTimeoutEnvVar, "50ms")
	projectRoot := devinProjectRoot(repoRoot)
	userRoot := devinUserRoot(home)
	for _, root := range []string{projectRoot, userRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	handwritten := "{\n  // kept by hand\n  \"mcpServers\": {\n    \"mine\": {\"command\": \"mine\"}\n  }\n}\n"
	files := map[string]string{
		filepath.Join(projectRoot, harness.DevinMcpConfigFile): handwritten,
		filepath.Join(userRoot, harness.DevinMcpConfigFile):    handwritten,
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	letGoProject := startDevinLockHolder(t, projectRoot, stateRoot)
	defer letGoProject()
	letGoUser := startDevinLockHolder(t, userRoot, stateRoot)
	defer letGoUser()

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{
		devinResolved("alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved("machine", ir.NewMcpServer("machine", "machine-binary", nil, nil, nil, ir.SourceRef{}), true),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	timeouts := findDiags(report.Diags, diag.CodeOwnershipLockTimeout)
	if len(timeouts) != 2 {
		t.Fatalf("two contended roots produced %d ownership.lock-timeout diagnostics, want exactly 2 (one per root): %v",
			len(timeouts), report.Diags)
	}
	named := strings.Join(func() []string {
		out := make([]string, 0, len(timeouts))
		for _, d := range timeouts {
			out = append(out, d.Message)
		}
		return out
	}(), "\n")
	for _, root := range []string{projectRoot, userRoot} {
		if !strings.Contains(named, root) {
			t.Fatalf("no timeout diagnostic names the skipped root %q:\n%s", root, named)
		}
	}
	for path, want := range files {
		if got := readFileString(t, path); got != want {
			t.Fatalf("a skipped root's aggregate changed at %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
		}
	}
}

// TestAdversarialAMachineScopeTimeoutDoesNotBlockTheRepoRoot is the mirror of
// TestDevinLockTimeoutSkipsExactlyThatRoot: the existing test blocks the repo
// root and checks the machine root survives, so the machine-root direction was
// unasserted. The two roots reach the reconciler in one loop over output keys,
// so an early `return report` on the wrong branch would be visible only in one
// of the two orders.
func TestAdversarialAMachineScopeTimeoutDoesNotBlockTheRepoRoot(t *testing.T) {
	repoRoot, home := devinEnv(t)
	t.Setenv(ownership.LockTimeoutEnvVar, "50ms")
	userRoot := devinUserRoot(home)
	if err := os.MkdirAll(userRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	handwritten := "{\n  \"mcpServers\": {\n    \"mine\": {\"command\": \"mine\"}\n  }\n}\n"
	userFile := filepath.Join(userRoot, harness.DevinMcpConfigFile)
	if err := os.WriteFile(userFile, []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}

	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	letGo := startDevinLockHolder(t, userRoot, stateRoot)
	defer letGo()

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{
		devinResolved("alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved("machine", ir.NewMcpServer("machine", "machine-binary", nil, nil, nil, ir.SourceRef{}), true),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(findDiags(report.Diags, diag.CodeOwnershipLockTimeout)); got != 1 {
		t.Fatalf("ownership.lock-timeout diagnostics = %d, want exactly 1: %v", got, report.Diags)
	}
	if got := readFileString(t, userFile); got != handwritten {
		t.Fatalf("the skipped machine-scope root changed:\n%s", got)
	}
	repoFile := filepath.Join(devinProjectRoot(repoRoot), harness.DevinMcpConfigFile)
	if got := readFileString(t, repoFile); !strings.Contains(got, `"alpha"`) {
		t.Fatalf("a machine-scope timeout blocked the repo root:\n%s", got)
	}
}
