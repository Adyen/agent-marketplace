//go:build unix

package materialize

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// The crash story for a section that now spans SEVERAL journaled generations.
//
// B2's eight crash-injection points all live inside one Apply. B3 changed the
// shape: one held descriptor now covers Recover plus one Apply per aggregate
// file, so a process killed inside the section dies with generation N committed
// and generation N+1 never started. Three things have to be true of that state
// and none of them was asserted anywhere:
//
//   - the kernel released the lock, so the root is not wedged for the next run
//     (there is no stale-flock policy precisely because there is nothing to
//     clean up — but "there is nothing to clean up" is a claim about a MULTI-
//     GENERATION section now, not about one Apply);
//   - the file that DID commit keeps its ownership, so the next sync sees
//     owned-clean rather than drift and does not rewrite it with a misleading
//     "was edited by hand" warning;
//   - the file that did NOT commit is byte-identical to what it was, and the
//     next run finishes the job.
//
// A half-reconciled root is acceptable — aggregate updates are atomic per file,
// not per target — but a half-reconciled root that has lost ownership of the file
// it did commit is not.

const (
	devinKillChildEnv      = "AM_DEVIN_KILL_CHILD"
	devinKillRootEnv       = "AM_DEVIN_KILL_TARGET_ROOT"
	devinKillStateEnv      = "AM_DEVIN_KILL_STATE_ROOT"
	devinKillCommittedFile = "COMMITTED"
)

// TestDevinKillMidSectionChildProcess is not a test: it is the body of the child
// the crash test execs. It reconciles BOTH of the project root's aggregate files
// inside one session and SIGKILLs itself at the start of the second file's Apply,
// i.e. after the first file's generation is committed and before the second one's
// begins.
//
// The kill point is chosen from the lock's own events rather than from a timer:
// inside one session the acquisitions are Recover, then one Apply per file, and
// each of them is a reentry. The third reentry is therefore the second file's
// Apply, exactly, on every scheduling.
func TestDevinKillMidSectionChildProcess(t *testing.T) {
	if os.Getenv(devinKillChildEnv) == "" {
		t.Skip("not a child process invocation")
	}
	root := os.Getenv(devinKillRootEnv)
	t.Setenv("XDG_STATE_HOME", os.Getenv(devinKillStateEnv))

	work := &devinAggregateWork{}
	work.add(devinHookDesired(t, "echo committed"))
	work.add(devinMcpDesired(t, "never", "never-binary"))

	var mu sync.Mutex
	releases := 0
	devinLockAcquisitionHook = func(ev ownership.LockEvent) {
		// Counted on RELEASE, not on acquisition, for two reasons. A release is
		// unambiguously "a mutation just FINISHED", so the kill lands after a
		// commit rather than somewhere inside one — the eight points inside one
		// Apply are B2's, and this test is about the gap BETWEEN two of them. And
		// it is the one count that is identical whether or not the outer session
		// exists (with it the inner releases return to depth 1, without it to
		// depth 0), so a tree with the session removed still reaches the kill
		// point and this test keeps reporting the crash contract instead of
		// failing about the section shape.
		//
		// Release 1 is Recover's; release 2 is the first aggregate file's Apply.
		if ev.Kind != ownership.LockReleased {
			return
		}
		mu.Lock()
		releases++
		n := releases
		mu.Unlock()
		if n < 2 {
			return
		}
		// Announce first, so the parent knows the first generation is durable
		// and this is not simply a child that died early.
		_, _ = os.Stdout.WriteString(devinKillCommittedFile + "\n")
		_ = os.Stdout.Sync()
		// SIGKILL, not os.Exit: os.Exit runs no deferred function and closes
		// nothing gracefully, but SIGKILL is the case the design leans on — the
		// kernel, and only the kernel, drops the flock.
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}

	reconcileDevinAggregates(context.Background(), newDevinReport(),
		outputKey{target: hcc.TargetDevin, machineScope: false}, root, work, false)
	t.Fatal("the child was not killed inside the section")
}

// devinHookDesired builds one desired hooks.v1.json entry.
func devinHookDesired(t *testing.T, command string) devinEntry {
	t.Helper()
	raw, err := devinCanonicalJSON([]byte(`{"type":"command","command":"` + command + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := devinHookIdentity("PostToolUse", raw)
	if err != nil {
		t.Fatal(err)
	}
	return devinEntry{
		rel:       harness.DevinProjectHooksFile,
		kind:      ownership.KindHook,
		container: "PostToolUse",
		raw:       raw,
		identity:  identity,
		resource:  "resource-hook",
	}
}

// TestAdversarialSIGKILLBetweenTwoGenerationsLeavesTheRootUsable is the crash
// item: a real SIGKILL inside a section that spans two generations.
func TestAdversarialSIGKILLBetweenTwoGenerationsLeavesTheRootUsable(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// A handwritten aggregate in the file that will NOT be reached, so "left
	// exactly as found" is asserted against bytes a user would recognize.
	handwritten := "{\n  // mine\n  \"mcpServers\": {\n    \"mine\": {\"command\": \"mine\"}\n  }\n}\n"
	mcpFile := filepath.Join(root, harness.DevinMcpConfigFile)
	if err := os.WriteFile(mcpFile, []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}
	// The XDG root, not ownership.StateRoot(): StateRoot() already appends
	// "agent-marketplace", so handing it back through XDG_STATE_HOME would give
	// the child a DIFFERENT state root from the parent and quietly make every
	// assertion about the parent's record meaningless.
	xdgState := os.Getenv("XDG_STATE_HOME")

	cmd := exec.Command(os.Args[0], "-test.run=^TestDevinKillMidSectionChildProcess$", "-test.timeout=120s")
	cmd.Env = append(os.Environ(),
		devinKillChildEnv+"=1",
		devinKillRootEnv+"="+root,
		devinKillStateEnv+"="+xdgState,
		ownership.LockTimeoutEnvVar+"=60s",
	)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	committed := scanFor(scanner, devinKillCommittedFile)
	waitErr := cmd.Wait()
	if !committed {
		t.Fatalf("the child never reached the kill point (exit: %v)", waitErr)
	}
	if waitErr == nil {
		t.Fatal("the child exited cleanly; it was supposed to be SIGKILLed inside the section")
	}

	// 1. The kernel released the lock. A fail-fast acquisition proves it without
	// waiting for anything.
	survivor, err := ownership.Open(root, ownership.WithLockTimeout(0))
	if err != nil {
		t.Fatal(err)
	}
	release, err := survivor.Lock()
	if err != nil {
		t.Fatalf("a SIGKILLed holder wedged the root: %v.\n"+
			"There is no stale-lock policy because a flock cannot go stale; that claim now has to hold "+
			"for a section spanning several generations, not just one Apply.", err)
	}
	release()

	// 2. The file that committed keeps its ownership: the record describes it,
	// and the entry classifies owned-clean rather than as drift.
	hooksFile := filepath.Join(root, harness.DevinProjectHooksFile)
	hooksRaw, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatalf("the committed generation did not survive the kill: %v", err)
	}
	if !strings.Contains(string(hooksRaw), "echo committed") {
		t.Fatalf("the committed generation's content is missing:\n%s", hooksRaw)
	}
	reader, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := reader.Proof(harness.DevinProjectHooksFile)
	if err != nil {
		t.Fatalf("proof for the committed file: %v", err)
	}
	if got, want := proof.ContentDigest(), ownership.Digest(hooksRaw); got != want {
		t.Fatalf("after a kill inside the section, the record for the COMMITTED file describes %s but the "+
			"file is %s; the next sync would call a committed write drift and rewrite it with a "+
			"misleading 'was edited by hand' warning", got, want)
	}
	if proof.OwnedCount() == 0 {
		t.Fatal("the committed file's entries became unmanaged after the kill")
	}

	// 3. The file that was never reached is byte-identical.
	if got := readFileString(t, mcpFile); got != handwritten {
		t.Fatalf("a file the section never reached was changed:\n--- want ---\n%s\n--- got ---\n%s", handwritten, got)
	}

	// 4. The next run resolves whatever was left and finishes the job, without a
	// single error diagnostic.
	work := &devinAggregateWork{}
	work.add(devinHookDesired(t, "echo committed"))
	work.add(devinMcpDesired(t, "never", "never-binary"))
	report := reconcileDevinAggregates(context.Background(), newDevinReport(),
		outputKey{target: hcc.TargetDevin, machineScope: false}, root, work, false)
	requireNoErrorDiags(t, report.Diags)
	if got := readFileString(t, mcpFile); !strings.Contains(got, `"never"`) || !strings.Contains(got, `"mine"`) {
		t.Fatalf("the run after the kill neither finished the job nor preserved the handwritten entry:\n%s", got)
	}
	after, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := after.ReadJournal()
	if err != nil {
		t.Fatalf("read journal after the recovering run: %v", err)
	}
	if journal != nil {
		t.Fatalf("an in-flight journal survived the recovering run: %+v", journal)
	}
	staged, err := filepath.Glob(filepath.Join(root, "*.am-staged-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 0 {
		t.Fatalf("staged files survived the kill and the recovering run: %v", staged)
	}
}
