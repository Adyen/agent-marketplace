package materialize

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/testutil"
)

// This file is bead agent-marketplace-npwr's (B4's) acceptance suite: each of the
// three repair mutators now runs requireCompleteGeneration, its re-derivation from
// the current file, and ownership.Apply inside ONE held write session.
//
// Three rules shape it, and they are the same three B3's suite states.
//
// 1. NOTHING IS TIMED AND NOTHING SLEEPS. The cross-process test constructs its
// interleaving with a pipe rendezvous and detects an overlap with an O_EXCL
// witness file, so a violation is caught by the kernel refusing a create rather
// than by a test being lucky.
//
// 2. THE SERIALIZATION CLAIM IS MADE ACROSS REAL PROCESSES, because an in-process
// test cannot distinguish flock from a package-level mutex keyed by root, and the
// defect this bead closes is a two-terminal defect. The un-skipped repro
// (TestAdversarialConcurrentConflictReplacesForgeOwnership) is necessary but not
// sufficient for exactly that reason.
//
// 3. THE BOUNDARY IS COUNTED, not inferred. On an uncontended root a section
// around Apply alone and a section around the whole action write identical bytes;
// they differ only in acquisitions and depth, so that is what is asserted.

// ---------------------------------------------------------------------------
// 1. The section boundary of each mutator
// ---------------------------------------------------------------------------

// repairLockCounts is what repairLockHook observes for one action.
type repairLockCounts struct {
	mu        sync.Mutex
	acquired  int
	reentered int
	released  int
	maxDepth  int
}

func (c *repairLockCounts) observe(ev ownership.LockEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch ev.Kind {
	case ownership.LockAcquired:
		c.acquired++
	case ownership.LockReentered:
		c.reentered++
	case ownership.LockReleased:
		c.released++
	}
	if ev.Depth > c.maxDepth {
		c.maxDepth = ev.Depth
	}
}

// snapshot returns the counters, and resets them so one test can measure several
// actions independently.
func (c *repairLockCounts) snapshot() (acquired, reentered, released, maxDepth int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	acquired, reentered, released, maxDepth = c.acquired, c.reentered, c.released, c.maxDepth
	c.acquired, c.reentered, c.released, c.maxDepth = 0, 0, 0, 0
	return
}

// installRepairLockCounter routes the repair path's lock events into a counter.
func installRepairLockCounter(t *testing.T) *repairLockCounts {
	t.Helper()
	counts := &repairLockCounts{}
	repairLockHook = counts.observe
	t.Cleanup(func() { repairLockHook = nil })
	return counts
}

// requireOneRepairSection is the shape every repair action must produce: ONE
// acquisition (the session), ONE reentry (the single ownership.Apply inside it), a
// maximum depth of two, and a full release.
//
// Each half of that is a distinct failure. Two acquisitions mean the guard and the
// re-derivation are outside the section that protects the commit — the TOCTOU this
// bead closes. Zero reentries mean no mutation ran. A depth above two means two
// mutations were nested rather than sequential, which is what
// ownership.ErrNestedMutation refuses and what would corrupt the per-root journal
// slot. A residual depth means the lock was leaked past the action, which is
// exactly what must never happen across a prompt.
func requireOneRepairSection(t *testing.T, action string, counts *repairLockCounts) {
	t.Helper()
	acquired, reentered, released, maxDepth := counts.snapshot()
	if acquired != 1 {
		t.Fatalf("%s took the ownership lock %d times, want exactly 1.\n"+
			"More than one means requireCompleteGeneration and the candidate re-derivation are "+
			"outside the section that protects the Apply, so another writer can commit in between "+
			"and the guard proves nothing.", action, acquired)
	}
	if reentered != 1 {
		t.Fatalf("%s reentered the lock %d times, want exactly 1 (the single ownership.Apply "+
			"inside the held session)", action, reentered)
	}
	if maxDepth != 2 {
		t.Fatalf("%s reached lock depth %d, want 2: one session plus one mutation. A deeper stack "+
			"would mean nested mutations, which ownership.ErrNestedMutation refuses.", action, maxDepth)
	}
	if released != reentered+acquired {
		t.Fatalf("%s released %d levels for %d acquisitions; a level left held is a lock leaked "+
			"past the action and therefore across doctor's next prompt",
			action, released, reentered+acquired)
	}
}

// TestDevinRepairSectionIsOneAcquisitionPerAction is the section BOUNDARY test for
// all three mutators, and the one that goes red the moment a session is removed.
//
// It also pins the two negatives that matter as much as the positive: the PREVIEW
// takes no lock at all (it is read-only by construction, and acquiring would create
// locks/<rootKey>.lock on an off-TTY `doctor --repair`, which must mutate nothing),
// and an Adopt with an empty selection — what doctor calls when a user declined
// every adoption prompt — acquires nothing either.
func TestDevinRepairSectionIsOneAcquisitionPerAction(t *testing.T) {
	t.Run("adopt", func(t *testing.T) {
		repoRoot, _ := devinEnv(t)
		root := devinProjectRoot(repoRoot)
		selection := devinMcpSelection("managed")
		if _, err := Run(context.Background(), repoRoot, selection); err != nil {
			t.Fatalf("seed sync: %v", err)
		}
		dropOwnershipState(t, root)

		counts := installRepairLockCounter(t)
		preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		if acquired, reentered, _, _ := counts.snapshot(); acquired != 0 || reentered != 0 {
			t.Fatalf("the preview took the lock (%d acquisitions, %d reentries); DevinRepairPreview "+
				"is read-only by construction and off-TTY `doctor --repair` must create no lock file",
				acquired, reentered)
		}

		file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
		if _, err := file.Adopt(nil); err != nil {
			t.Fatalf("empty adopt: %v", err)
		}
		if acquired, _, _, _ := counts.snapshot(); acquired != 0 {
			t.Fatalf("an adoption of nothing took the lock %d times; a user who declined every "+
				"prompt must leave no trace", acquired)
		}
		if _, err := file.Adopt([]int{0}); err != nil {
			t.Fatalf("adopt: %v", err)
		}
		requireOneRepairSection(t, "Adopt", counts)
	})

	t.Run("replace a conflict", func(t *testing.T) {
		repoRoot, _ := devinEnv(t)
		writeDevinMcp(t, repoRoot, "{\n  \"mcpServers\": {\n"+
			"    \"aaa\": { \"command\": \"/hand/aaa\" }\n  }\n}\n")
		selection := devinMcpSelection("aaa")
		if _, err := Run(context.Background(), repoRoot, selection); err != nil {
			t.Fatalf("seed sync: %v", err)
		}

		counts := installRepairLockCounter(t)
		_, file := conflictPreviewFor(t, repoRoot, selection)
		if file == nil || len(file.Conflicts) != 1 {
			t.Fatal("the fixture did not produce exactly one conflict candidate")
		}
		if acquired, _, _, _ := counts.snapshot(); acquired != 0 {
			t.Fatalf("the conflict preview took the lock %d times", acquired)
		}
		if _, err := file.ReplaceConflict(0, true); err != nil {
			t.Fatalf("replace: %v", err)
		}
		requireOneRepairSection(t, "ReplaceConflict", counts)
	})

	t.Run("remove an orphan", func(t *testing.T) {
		repoRoot, _ := devinEnv(t)
		root := devinProjectRoot(repoRoot)
		if _, err := Run(context.Background(), repoRoot, devinMcpSelection("managed")); err != nil {
			t.Fatalf("seed sync: %v", err)
		}
		dropOwnershipState(t, root)

		counts := installRepairLockCounter(t)
		// Nothing is desired any more, so the entry sync wrote is an
		// evidence-backed orphan candidate.
		preview, err := DevinRepairPreview(context.Background(), repoRoot, nil)
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
		if len(file.Orphans) != 1 {
			t.Fatalf("orphan candidates = %d, want 1", len(file.Orphans))
		}
		if acquired, _, _, _ := counts.snapshot(); acquired != 0 {
			t.Fatalf("the orphan preview took the lock %d times", acquired)
		}
		if _, err := file.RemoveOrphan(0, true); err != nil {
			t.Fatalf("remove orphan: %v", err)
		}
		requireOneRepairSection(t, "RemoveOrphan", counts)
	})
}

// TestDevinRepairRefusesAnUnreplaceableConflictWithoutTakingTheLock pins that the
// MCP-only replace fence is answered before any acquisition. The refusal is
// decided from the aggregate's name and the entry's identity alone, so it must
// not create locks/<rootKey>.lock and must not block behind another writer.
func TestDevinRepairRefusesAnUnreplaceableConflictWithoutTakingTheLock(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	hookID := ownership.EntryID{
		Kind:           ownership.KindHook,
		Container:      "PostAgentResponse",
		IdentityDigest: ownership.Digest([]byte("hook-identity")),
	}
	candidates := ownership.ValueConflictCandidates(ownership.Proof{},
		[]ownership.EntryValue{{ID: hookID, ValueDigest: ownership.Digest([]byte("existing"))}},
		[]ownership.EntryValue{{ID: hookID, ValueDigest: ownership.Digest([]byte("desired"))}})
	if len(candidates) != 1 {
		t.Fatalf("ownership nominated %d hook candidates, want 1", len(candidates))
	}
	store, err := ownership.Open(root, ownership.WithLockObserver(observeRepairLock))
	if err != nil {
		t.Fatal(err)
	}
	counts := installRepairLockCounter(t)
	file := &RepairFile{
		Target: hcc.TargetDevin, OutDir: root, Rel: harness.DevinProjectHooksFile,
		Conflicts: []ConflictCandidate{{Candidate: candidates[0]}},
		store:     store,
	}
	if _, err := file.ReplaceConflict(0, true); err == nil {
		t.Fatal("a hook was replaced; the MCP-only replace fence must reject artificial hook candidates")
	}
	if acquired, _, _, _ := counts.snapshot(); acquired != 0 {
		t.Fatalf("a refused-by-scope replacement took the lock %d times", acquired)
	}
	if _, err := os.Lstat(store.LockPath()); !os.IsNotExist(err) {
		t.Fatalf("a refused-by-scope replacement created %s (%v)", store.LockPath(), err)
	}
}

// ---------------------------------------------------------------------------
// 2. A sync and a doctor --repair on one root, in two real processes
// ---------------------------------------------------------------------------

const (
	devinRepairXProcModeEnv    = "AM_DEVIN_REPAIR_XPROC_MODE"
	devinRepairXProcRepoEnv    = "AM_DEVIN_REPAIR_XPROC_REPO_ROOT"
	devinRepairXProcWitnessEnv = "AM_DEVIN_REPAIR_XPROC_WITNESS"
	devinRepairXProcReady      = "READY"
	devinRepairXProcDone       = "DONE"
	devinRepairXProcEntered    = "ENTERED"
	devinRepairXProcOverlap    = "OVERLAP"
)

// sectionWitness detects two processes being inside the per-root section at the
// same time, WITHOUT any timing.
//
// enter() creates a file with O_CREATE|O_EXCL and leave() removes it, and both
// happen inside a held section. O_EXCL is decided by the kernel against the
// directory entry, so if the file already exists then some other process is
// between its own enter and leave — i.e. two sections overlapped. There is no
// window to sample and no duration to guess: an overlap of one nanosecond is
// caught exactly as reliably as one of a second.
type sectionWitness struct {
	path    string
	entered atomic.Bool
	held    atomic.Bool
	overlap atomic.Bool
}

func (w *sectionWitness) enter() {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			w.overlap.Store(true)
		}
		return
	}
	_ = f.Close()
	w.entered.Store(true)
	w.held.Store(true)
}

func (w *sectionWitness) leave() {
	if w.held.CompareAndSwap(true, false) {
		_ = os.Remove(w.path)
	}
}

// observeSection turns lock events into witness transitions. Only the OUTERMOST
// acquisition and release matter: a reentry is the same section at a deeper depth.
func (w *sectionWitness) observe(ev ownership.LockEvent) {
	switch {
	case ev.Kind == ownership.LockAcquired:
		w.enter()
	case ev.Kind == ownership.LockReleased && ev.Depth == 0:
		w.leave()
	}
}

// TestDevinRepairCrossProcessChild is not a test: it is the body of the child
// processes TestDevinSyncAndRepairSerializeAcrossProcesses execs, inert unless its
// mode env var is set.
//
// One child runs a real `sync` reconciliation of the root and the other runs a
// real `doctor --repair` conflict replacement on the same root. Both announce
// readiness and park on a pipe read, so the parent releases them together.
func TestDevinRepairCrossProcessChild(t *testing.T) {
	mode := os.Getenv(devinRepairXProcModeEnv)
	if mode == "" {
		t.Skip("not a child process invocation")
	}
	repoRoot := os.Getenv(devinRepairXProcRepoEnv)
	witness := &sectionWitness{path: os.Getenv(devinRepairXProcWitnessEnv)}
	ownedAggregateLockAcquisitionHook = witness.observe
	repairLockHook = witness.observe

	selection := []resolver.ResolvedArtifact{
		devinResolved("aaa", ir.NewMcpServer("aaa", "aaa-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved("zzz", ir.NewMcpServer("zzz", "zzz-binary", nil, nil, nil, ir.SourceRef{}), false),
	}

	// Everything expensive happens BEFORE the rendezvous — the preview in
	// particular, which is read-only and takes no lock — so that after the
	// release each child has nothing left but its section.
	var repair *RepairFile
	if mode == "repair" {
		preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
		if err != nil {
			t.Fatalf("child preview: %v", err)
		}
		for _, file := range preview.Files {
			if file.Rel == harness.DevinMcpConfigFile {
				repair = file
			}
		}
		if repair == nil || len(repair.Conflicts) == 0 {
			t.Fatalf("child preview produced no conflict candidate for %s", harness.DevinMcpConfigFile)
		}
	}

	if _, err := os.Stdout.WriteString(devinRepairXProcReady + "\n"); err != nil {
		t.Fatalf("child announce: %v", err)
	}
	_, _ = os.Stdin.Read(make([]byte, 1))

	switch mode {
	case "sync":
		report, err := Run(context.Background(), repoRoot, selection)
		if err != nil {
			t.Fatalf("child sync: %v", err)
		}
		for _, d := range report.Diags {
			if d.Severity == diag.SeverityError {
				t.Fatalf("child sync: %s: %s", d.Code, d.Message)
			}
		}
	case "repair":
		// A stale candidate is a CORRECT outcome for the loser of the race: the
		// sync it lost to may have made the entry owned and clean, at which point
		// there is no conflict left to replace. What must never happen is a
		// replacement decided from a file that has since been rewritten, and that
		// is what the witness and the parent's record check assert.
		if _, err := repair.ReplaceConflict(0, true); err != nil && !errors.Is(err, ErrRepairCandidateStale) {
			t.Fatalf("child repair: %v", err)
		}
	default:
		t.Fatalf("unknown child mode %q", mode)
	}

	if witness.overlap.Load() {
		if _, err := os.Stdout.WriteString(devinRepairXProcOverlap + "\n"); err != nil {
			t.Fatalf("child announce overlap: %v", err)
		}
	}
	if witness.entered.Load() {
		if _, err := os.Stdout.WriteString(devinRepairXProcEntered + "\n"); err != nil {
			t.Fatalf("child announce entered: %v", err)
		}
	}
	if _, err := os.Stdout.WriteString(devinRepairXProcDone + "\n"); err != nil {
		t.Fatalf("child announce done: %v", err)
	}
}

// TestDevinSyncAndRepairSerializeAcrossProcesses asserts that a `sync` and a
// `doctor --repair` racing on ONE canonical root serialize rather than interleave,
// in two real PROCESSES holding two separate open file descriptions — the
// two-terminal case the parent bead reported, and one an in-process mutex is worth
// exactly nothing against.
//
// # What it does NOT pin, measured rather than assumed
//
// It is not a test of B4's SECTION, and its first docstring claimed otherwise.
// With [RepairFile.withRepairSession] replaced by a pass-through it STILL PASSES
// (agent-marketplace-npwr review measured this), because the witness observes lock
// HOLDINGS and not sections: with B4 deleted the repair child still acquires
// inside ownership.Apply, the sync child still holds B3's whole outer session, and
// the two holdings still exclude each other. So what goes red here is B1's lock or
// B3's session, not this bead's boundary. With tryLockFile stubbed to return nil
// it fails, which is what makes it a real test of the mechanism.
//
// B4's boundary is pinned by TestDevinRepairSectionIsOneAcquisitionPerAction
// (acquisition count and depth) and its behavioural consequence by
// TestAdversarialConcurrentRepairsMustNotLoseAReplacement, which fails 20/20 with
// the session removed. This test's value is the cross-process claim itself, which
// neither of those makes.
//
// Overlap is detected by an O_EXCL witness file created inside each section
// (see sectionWitness), so nothing is timed and nothing sleeps. Two further
// properties are asserted afterwards, because "they did not overlap" is not by
// itself enough: the durable record must still describe the live bytes, and both
// entries must still be in the file — a serialized loser is allowed to be refused
// as stale, never to lose someone else's entry.
func TestDevinSyncAndRepairSerializeAcrossProcesses(t *testing.T) {
	if os.Getenv(devinRepairXProcModeEnv) != "" {
		t.Skip("this is the child-process body")
	}
	// Each trial forks two processes that each run a full reconciliation, so the
	// budget is the same order as B3's cross-process repro.
	trials := testutil.Trials(testutil.FullTrials/8, testing.Short())
	for trial := 0; trial < trials; trial++ {
		repoRoot, _ := devinEnv(t)
		root := devinProjectRoot(repoRoot)
		// Two hand-written entries whose keys match the desired resources but
		// whose values differ: the conflict class, which is the destructive one.
		writeDevinMcp(t, repoRoot, "{\n  \"mcpServers\": {\n"+
			"    \"aaa\": { \"command\": \"/hand/aaa\" },\n"+
			"    \"zzz\": { \"command\": \"/hand/zzz\" }\n  }\n}\n")
		selection := []resolver.ResolvedArtifact{
			devinResolved("aaa", ir.NewMcpServer("aaa", "aaa-binary", nil, nil, nil, ir.SourceRef{}), false),
			devinResolved("zzz", ir.NewMcpServer("zzz", "zzz-binary", nil, nil, nil, ir.SourceRef{}), false),
		}
		if _, err := Run(context.Background(), repoRoot, selection); err != nil {
			t.Fatalf("trial %d: seed sync: %v", trial, err)
		}

		witnessPath := filepath.Join(t.TempDir(), "section.witness")
		runDevinRepairRacers(t, trial, repoRoot, witnessPath)

		if _, err := os.Lstat(witnessPath); !os.IsNotExist(err) {
			t.Fatalf("trial %d: the section witness %s survived (%v); a section was left open",
				trial, witnessPath, err)
		}
		assertRecordDescribesDevinFile(t, trial, root, harness.DevinMcpConfigFile)
		got := readFileString(t, filepath.Join(root, harness.DevinMcpConfigFile))
		for _, key := range []string{"aaa", "zzz"} {
			if !strings.Contains(got, `"`+key+`"`) {
				t.Fatalf("trial %d: entry %q vanished when a sync raced a repair:\n%s", trial, key, got)
			}
		}
	}
}

// runDevinRepairRacers starts the sync child and the repair child, waits until
// BOTH are parked at the rendezvous, releases them together, and fails if either
// reported an overlap or never entered a section at all.
//
// "Never entered a section" is a failure and not a skip: a trial in which one
// racer did no work would prove nothing while looking green, which is precisely
// how a vacuous concurrency test is built.
func runDevinRepairRacers(t *testing.T, trial int, repoRoot, witnessPath string) {
	t.Helper()
	type child struct {
		mode    string
		cmd     *exec.Cmd
		stdin   io.WriteCloser
		scanner *bufio.Scanner
	}
	var children []*child
	for _, mode := range []string{"sync", "repair"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDevinRepairCrossProcessChild$", "-test.timeout=120s")
		cmd.Env = append(os.Environ(),
			devinRepairXProcModeEnv+"="+mode,
			devinRepairXProcRepoEnv+"="+repoRoot,
			devinRepairXProcWitnessEnv+"="+witnessPath,
			// A generous timeout: the loser of this race must WAIT for the winner
			// and then proceed, not report a timeout. A short one would turn the
			// property under test into a flake.
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
		children = append(children, &child{mode: mode, cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout)})
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	for _, c := range children {
		if !scanFor(c.scanner, devinRepairXProcReady) {
			t.Fatalf("trial %d: the %s child never announced readiness", trial, c.mode)
		}
	}
	for _, c := range children {
		_, _ = c.stdin.Write([]byte("\n"))
		_ = c.stdin.Close()
	}
	for _, c := range children {
		overlap, entered, done := false, false, false
		for c.scanner.Scan() {
			line := c.scanner.Text()
			switch {
			case strings.Contains(line, devinRepairXProcOverlap):
				overlap = true
			case strings.Contains(line, devinRepairXProcEntered):
				entered = true
			case strings.Contains(line, devinRepairXProcDone):
				done = true
			}
			if done {
				break
			}
		}
		if overlap {
			t.Fatalf("trial %d: the %s child was inside the per-root section while the other "+
				"process was too. A sync and a doctor --repair on one root must serialize: "+
				"interleaving is the P0 this bead closes.", trial, c.mode)
		}
		if !entered {
			t.Fatalf("trial %d: the %s child never entered a section, so the trial proves nothing",
				trial, c.mode)
		}
		if !done {
			t.Fatalf("trial %d: the %s child did not finish", trial, c.mode)
		}
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("trial %d: the %s child exited with an error: %v", trial, c.mode, err)
		}
	}
}

// TestDevinRepairIsRefusedWhileAnotherProcessHoldsTheRoot is the fail-closed half
// of the same property, and it is what makes the "no lock across a prompt" test
// meaningful: a repair really does need the lock, so observing that the lock is
// FREE while doctor waits for an answer is a statement about the code and not
// about a lock nobody takes.
//
// A real child process holds the root's lock. Every one of the three actions must
// then be refused with ownership's timeout error and leave the aggregate
// byte-identical — no partial edit, and no ownership generation.
func TestDevinRepairIsRefusedWhileAnotherProcessHoldsTheRoot(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)

	// Seed a managed generation with three entries. After dropping the durable
	// record, one stays desired and exact (adoption), one is hand-edited to
	// become a conflict (desired but with a different value), and one is no
	// longer desired (orphan) but still has safety-backup evidence from the
	// seed generation.
	if _, err := Run(context.Background(), repoRoot, devinMcpSelection("aaa", "bbb", "ccc")); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	dropOwnershipState(t, root)

	path := filepath.Join(root, harness.DevinMcpConfigFile)
	edited := strings.ReplaceAll(readFileString(t, path), "bbb-binary", "/hand/bbb")
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	selection := devinMcpSelection("aaa", "bbb")
	// The timeout is set BEFORE the preview, not after it: a Store resolves its
	// lock timeout once, in ownership.Open, so an env var exported after the
	// preview has already opened the store is read by nobody and the refusal
	// below burns the 30s default instead of 50ms.
	t.Setenv(ownership.LockTimeoutEnvVar, "50ms")
	// The preview is taken BEFORE the holder starts, because a preview takes no
	// lock and must not be part of what is refused.
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if len(file.Adoptions) != 1 || len(file.Conflicts) != 1 || len(file.Orphans) != 1 {
		t.Fatalf("fixture produced adoptions=%d conflicts=%d orphans=%d, want 1 each",
			len(file.Adoptions), len(file.Conflicts), len(file.Orphans))
	}

	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	letGo := startDevinLockHolder(t, root, stateRoot)
	defer letGo()

	before := readFileString(t, path)
	if _, err := file.ReplaceConflict(0, true); !errors.Is(err, ownership.ErrLockTimeout) {
		t.Fatalf("ReplaceConflict while another process held the root: err = %v, want ownership.ErrLockTimeout", err)
	}
	if got := readFileString(t, path); got != before {
		t.Fatalf("a refused replacement changed the aggregate:\n--- want ---\n%s\n--- got ---\n%s", before, got)
	}
	if _, err := file.Adopt([]int{0}); !errors.Is(err, ownership.ErrLockTimeout) {
		t.Fatalf("Adopt while another process held the root: err = %v, want ownership.ErrLockTimeout", err)
	}
	if got := readFileString(t, path); got != before {
		t.Fatalf("a refused adoption changed the aggregate:\n%s", got)
	}
	if _, err := file.RemoveOrphan(0, true); !errors.Is(err, ownership.ErrLockTimeout) {
		t.Fatalf("RemoveOrphan while another process held the root: err = %v, want ownership.ErrLockTimeout", err)
	}
	if got := readFileString(t, path); got != before {
		t.Fatalf("a refused orphan removal changed the aggregate:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// 3. The behavioural consequence: a repair may be refused, never overwritten
// ---------------------------------------------------------------------------

// TestAdversarialConcurrentRepairsMustNotLoseAReplacement is the BEHAVIOURAL
// assertion the un-skipped repro cannot make, and it is the test that measurably
// goes red when the session around a mutator is removed.
//
// The measurement matters, so it is written down. With
// [RepairFile.withRepairSession] replaced by a pass-through — i.e. deleting B4
// while leaving B1's lock and B2's lock-inside-Apply in place —
// TestAdversarialConcurrentConflictReplacesForgeOwnership STILL PASSES at
// -count=50 -race. Apply's own lock keeps the record internally consistent even
// when two repairs race: the loser's recordedEntries() predates the winner's
// commit, so the loser commits a record that simply OMITS the winner's entry
// rather than one that claims a digest the file lacks. The repro asserts the
// second thing and cannot see the first.
//
// What is lost in that window is the winner's WRITE. The loser staged its bytes
// from a read that predates the winner's rename, so its Apply publishes a document
// in which the winner's replacement never happened — a silent lost update of a
// destructive, individually-confirmed action. The user answered "y" twice, both
// answers were accepted, and one of the two entries is still the hand-written
// value.
//
// With the whole read-modify-commit inside one session the loser runs entirely
// after the winner: it re-derives from the published file, finds its own key still
// conflicting (a different key), and commits the union. So both confirmed
// replacements are in the file and the record describes it.
func TestAdversarialConcurrentRepairsMustNotLoseAReplacement(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	path := writeDevinMcp(t, repoRoot, "{\n  \"mcpServers\": {\n"+
		"    \"aaa\": { \"command\": \"/hand/aaa\" },\n"+
		"    \"zzz\": { \"command\": \"/hand/zzz\" }\n  }\n}\n")
	selection := devinMcpSelection("aaa", "zzz")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}

	// Two independent previews, therefore two independent Stores: two open file
	// descriptions that genuinely exclude each other in the kernel.
	_, first := conflictPreviewFor(t, repoRoot, selection)
	_, second := conflictPreviewFor(t, repoRoot, selection)
	if first == nil || second == nil || len(first.Conflicts) != 2 || len(second.Conflicts) != 2 {
		t.Fatal("the fixture did not produce two conflict candidates in each preview")
	}

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; _, errs[0] = first.ReplaceConflict(0, true) }()
	go func() { defer wg.Done(); <-start; _, errs[1] = second.ReplaceConflict(1, true) }()
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: the two racers target different keys, so a serialized replacement "+
				"must succeed; got %v", i, err)
		}
	}
	got := readFileString(t, path)
	for _, want := range []string{"aaa-binary", "zzz-binary"} {
		if !strings.Contains(got, want) {
			t.Fatalf("a confirmed replacement was silently lost: %q is missing after two concurrent "+
				"repairs. The user answered yes and the hand-written value survived:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"/hand/aaa", "/hand/zzz"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("a hand-written value %q survived a confirmed replacement:\n%s", unwanted, got)
		}
	}
	assertRecordDescribesDevinFile(t, 0, devinProjectRoot(repoRoot), harness.DevinMcpConfigFile)
}
