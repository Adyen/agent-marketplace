package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// This file is the command-level half of bead agent-marketplace-npwr (B4): the
// three repair mutators now hold the per-canonical-root ownership lock across
// their whole read-modify-commit, and the INTERACTIVE loop that drives them must
// hold nothing at all while a human is thinking.
//
// Why that is the constraint that matters most here. `doctor --repair` prompts per
// entry, and a prompt lasts as long as a person takes to answer it — seconds,
// minutes, or forever if they walked away. The lock is per canonical root, and one
// of the two Devin roots is the MACHINE-scope ~/.config/devin, so a lock held
// across that prompt would block every `sync` in every repository on the machine,
// including editor save hooks, for the whole of that time. Bounded contention on a
// millisecond-long section is the mechanism working; a lock waiting on a human is
// a deadlock with extra steps.
//
// The proof is structural — doctor prompts BETWEEN calls, so each answer produces
// its own acquisition — but structure is what regressions quietly change, so it is
// asserted: while the interactive loop is parked on stdin, another Store on the
// same root acquires the lock with a FAIL-FAST timeout. Fail-fast is deliberate:
// zero means exactly one non-blocking attempt, so the assertion cannot be
// satisfied by waiting and there is no duration to tune and nothing to sleep on.
//
// TestDoctorRepairIsRefusedWhileAnotherProcessHoldsTheLock is the other half, and
// it is what makes the first test non-vacuous: a repair really does take this
// lock, so observing it FREE at the prompt is a statement about where the section
// starts rather than about a lock nobody ever holds.

// promptWatcher forwards doctor's output to a buffer and signals every time a new
// confirmation prompt has been printed.
//
// It is how the test synchronizes with the interactive loop WITHOUT sleeping: the
// signal is the prompt itself. Detecting the prompt after the write but possibly
// before doctor blocks in Read cannot produce a false pass — a lock held across the
// prompt would already be held when the prompt was written.
type promptWatcher struct {
	mu       sync.Mutex
	buf      strings.Builder
	seen     int
	promptCh chan int
}

func newPromptWatcher() *promptWatcher {
	return &promptWatcher{promptCh: make(chan int, 8)}
}

func (w *promptWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf.Write(p)
	count := strings.Count(w.buf.String(), "[y/N]")
	newPrompts := count - w.seen
	w.seen = count
	w.mu.Unlock()
	for i := 0; i < newPrompts; i++ {
		select {
		case w.promptCh <- count:
		default:
		}
	}
	return len(p), nil
}

func (w *promptWatcher) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// requireLockIsFree asserts that some other Store can take the root's lock right
// now, with exactly one non-blocking attempt.
func requireLockIsFree(t *testing.T, outDir, when string) {
	t.Helper()
	// A SEPARATE Store, therefore a separate open file description: a flock
	// belongs to the description and not to the process, so this contends with
	// doctor's own store for real even though both live here.
	probe, err := ownership.Open(outDir, ownership.WithLockTimeout(0))
	if err != nil {
		t.Fatalf("open probe store: %v", err)
	}
	release, err := probe.Lock()
	if err != nil {
		t.Fatalf("%s: another writer could not take the ownership lock for %s: %v.\n"+
			"doctor --repair is holding the per-root lock across an interactive prompt. For the "+
			"machine-scope root ~/.config/devin that blocks every sync in every repository on the "+
			"machine for as long as the human takes to answer.", when, outDir, err)
	}
	release()
}

// TestDoctorRepairHoldsNoOwnershipLockWhileAwaitingConfirmation is the invariant
// "no lock may be held while doctor --repair waits on interactive input".
//
// The fixture has TWO conflicting entries, so the loop prompts twice and the
// assertion is made at both: before any mutation, and — the more interesting one —
// after the first replacement has committed. The second probe is therefore also
// the proof that a completed action RELEASES rather than leaking a depth level
// into the next prompt.
func TestDoctorRepairHoldsNoOwnershipLockWhileAwaitingConfirmation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareTwoDevinConflicts(t, repoRoot)
	outDir := filepath.Join(repoRoot, ".devin")
	// Fail-fast is the whole point of the probe below, and the env var BEATS
	// ownership.WithLockTimeout (it is the only knob a released binary has), so a
	// developer or a CI runner with AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT
	// exported would silently turn the probe into a 30s wait. It is pinned here
	// so "one non-blocking attempt" is a property of the test and not of the
	// environment it runs in. Nothing contends in this test, so doctor's own
	// acquisitions are unaffected.
	t.Setenv(ownership.LockTimeoutEnvVar, "0")

	stdinReader, stdinWriter := io.Pipe()
	watcher := newPromptWatcher()
	done := make(chan error, 1)
	go func() {
		done <- runDoctor(doctorOptions{
			out:     watcher,
			in:      stdinReader,
			isTTY:   true,
			repair:  true,
			probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
			resolve: staticResolve(selection),
		})
	}()

	for i, when := range []string{"at the first confirmation prompt", "at the second confirmation prompt"} {
		select {
		case <-watcher.promptCh:
		case err := <-done:
			t.Fatalf("doctor finished before prompt %d: %v\n%s", i+1, err, watcher.String())
		}
		requireLockIsFree(t, outDir, when)
		if _, err := stdinWriter.Write([]byte("y\n")); err != nil {
			t.Fatalf("answer prompt %d: %v", i+1, err)
		}
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("two confirmed replacements must exit 0: %v\n%s", err, watcher.String())
	}

	// And the answers were acted on: both hand-written values are gone, which is
	// what makes the two probes above probes of a real mutating run.
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-A", "-B"} {
		if strings.Contains(string(after), conflictSecret+suffix) {
			t.Fatalf("a confirmed replacement did not happen, so the prompts proved nothing:\n%s", after)
		}
	}
	if _, err := os.Lstat(filepath.Join(outDir, harness.DevinMcpConfigFile)); err != nil {
		t.Fatalf("the aggregate disappeared: %v", err)
	}
}

func TestDoctorRepairRefusesConfigChangeDuringConfirmation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nregistries: []\n")
	mcpPath, selection := prepareTwoDevinConflicts(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	stdinReader, stdinWriter := io.Pipe()
	watcher := newPromptWatcher()
	done := make(chan error, 1)
	go func() {
		done <- runDoctor(doctorOptions{
			out: watcher, in: stdinReader, isTTY: true, repair: true,
			probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
			resolve: staticResolve(selection),
		})
	}()

	select {
	case <-watcher.promptCh:
	case err := <-done:
		t.Fatalf("doctor finished before confirmation: %v\n%s", err, watcher.String())
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := stdinWriter.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("doctor did not reject changed selection policy: %v\n%s", err, watcher.String())
	}
	if after, err := os.ReadFile(mcpPath); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("doctor changed aggregate after config changed: %v\n%s", err, after)
	}
}

// TestDoctorRepairIsRefusedWhileAnotherProcessHoldsTheLock is the companion
// assertion: the repair path DOES take the per-root lock, so the freedom observed
// at the prompt above is a property of where the section begins.
//
// With another writer holding the root and a fail-fast timeout, a confirmed
// replacement must be refused and the user's file must be byte-identical. Failing
// closed is the contract: a repair that could not serialize must not write.
func TestDoctorRepairIsRefusedWhileAnotherProcessHoldsTheLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareTwoDevinConflicts(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	// Fail fast, so nothing waits and nothing is timed.
	t.Setenv(ownership.LockTimeoutEnvVar, "0")
	holder, err := ownership.Open(filepath.Join(repoRoot, ".devin"))
	if err != nil {
		t.Fatal(err)
	}
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("take the holder lock: %v", err)
	}
	defer release()

	watcher := newPromptWatcher()
	runErr := runDoctor(doctorOptions{
		out:     watcher,
		in:      strings.NewReader("y\ny\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if runErr == nil {
		t.Fatalf("a repair that could not acquire the root's lock reported success:\n%s", watcher.String())
	}
	// runDoctor collapses the per-action failures into a count and prints each
	// one, so the CAUSE is asserted on the output: the user must be told that
	// another process holds the lock, not merely that something failed.
	if !strings.Contains(watcher.String(), ownership.ErrLockTimeout.Error()) {
		t.Fatalf("the refusal did not name the ownership lock timeout:\n%s", watcher.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("a refused repair changed the user's aggregate:\n--- want ---\n%s\n--- got ---\n%s", before, after)
	}
}
