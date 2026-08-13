package materialize

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// This file is bead agent-marketplace-k5gi's (B6) acceptance suite: proving
// that Devin aggregate serialization holds across real OS processes, and that
// a fail-fast lock timeout refuses work rather than running unserialized.
//
// decision: The design document showed the child always setting
// AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT=60s. To reuse the same child for
// the zero-timeout test, an additional AM_B6_TIMEOUT env var is introduced
// and defaulted to 60s in the parent-facing helpers.

// ---------------------------------------------------------------------------
// 1. Child entry helpers
// ---------------------------------------------------------------------------

// devinB6HookDesired creates one desired hook entry for hooks.v1.json.
//
// decision: This helper is named devinB6HookDesired because devin_test.go's
// sibling devin_wrp8_kill_unix_test.go already declares a devinHookDesired with
// a different (single-argument) signature.
func devinB6HookDesired(t *testing.T, event, command string) devinEntry {
	t.Helper()
	raw, err := devinCanonicalJSON([]byte(`{"type":"command","command":"` + command + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := devinHookIdentity(event, raw)
	if err != nil {
		t.Fatal(err)
	}
	return devinEntry{
		rel:       harness.DevinProjectHooksFile,
		kind:      ownership.KindHook,
		container: event,
		raw:       raw,
		identity:  identity,
		resource:  "resource-" + event,
	}
}

func b6ChildEntry(t *testing.T, idx int) devinEntry {
	if idx < 4 {
		return devinMcpDesired(t, fmt.Sprintf("server-%d", idx), fmt.Sprintf("cmd-%d", idx))
	}
	return devinB6HookDesired(t, fmt.Sprintf("Event%d", idx), fmt.Sprintf("hook-cmd-%d", idx))
}

// ---------------------------------------------------------------------------
// 2. Cross-process child process
// ---------------------------------------------------------------------------

const (
	b6ChildIndexEnv   = "AM_B6_CHILD_INDEX"
	b6ChildTargetEnv  = "AM_B6_TARGET_ROOT"
	b6ChildStateEnv   = "AM_B6_STATE_ROOT"
	b6ChildWitnessEnv = "AM_B6_WITNESS"
	b6ChildTimeoutEnv = "AM_B6_TIMEOUT"

	b6Ready   = "READY"
	b6Done    = "DONE"
	b6Overlap = "OVERLAP"
	b6Refused = "REFUSED"
)

// TestDevinB6SerializationChildProcess is not a test: it is the body of the
// child processes TestDevinB6SerializationAcrossEightProcesses execs, inert
// unless its mode env var is set. It performs exactly one partial-run
// reconciliation of the given root, announcing readiness first so the parent
// can release all children together.
func TestDevinB6SerializationChildProcess(t *testing.T) {
	idxStr := os.Getenv(b6ChildIndexEnv)
	if idxStr == "" {
		t.Skip("not a child process invocation")
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		t.Fatalf("bad %s = %q: %v", b6ChildIndexEnv, idxStr, err)
	}
	targetRoot := os.Getenv(b6ChildTargetEnv)
	stateRoot := os.Getenv(b6ChildStateEnv)
	witnessPath := os.Getenv(b6ChildWitnessEnv)
	if targetRoot == "" {
		t.Fatalf("child %d: %s is empty", idx, b6ChildTargetEnv)
	}
	if witnessPath == "" {
		t.Fatalf("child %d: %s is empty; the overlap detector would be inert", idx, b6ChildWitnessEnv)
	}
	timeout := os.Getenv(b6ChildTimeoutEnv)
	if timeout == "" {
		timeout = "60s"
	}

	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv(ownership.LockTimeoutEnvVar, timeout)

	witness := &sectionWitness{path: witnessPath}
	devinLockAcquisitionHook = witness.observe
	defer func() { devinLockAcquisitionHook = nil }()

	entry := b6ChildEntry(t, idx)
	work := &devinAggregateWork{}
	work.add(entry)
	// Every child is a partial run: it must not prune entries written by siblings.
	work.markIncomplete(harness.DevinMcpConfigFile)
	work.markIncomplete(harness.DevinProjectHooksFile)

	fmt.Println(b6Ready)
	_, _ = os.Stdin.Read(make([]byte, 1))

	report := reconcileDevinAggregates(context.Background(), newDevinReport(),
		outputKey{target: hcc.TargetDevin, machineScope: false}, targetRoot, work, false)

	var lockTimeout bool
	for _, d := range report.Diags {
		if d.Code == diag.CodeOwnershipLockTimeout {
			lockTimeout = true
			continue
		}
		if d.Severity == diag.SeverityError {
			t.Fatalf("child %d: %s: %s", idx, d.Code, d.Message)
		}
	}

	if witness.overlap.Load() {
		fmt.Println(b6Overlap)
		return
	}

	if lockTimeout {
		fmt.Println(b6Refused)
		return
	}

	if !witness.entered.Load() {
		t.Fatalf("child %d never entered the per-root section", idx)
	}

	fmt.Println(b6Done)
}

// ---------------------------------------------------------------------------
// 3. Parent tests
// ---------------------------------------------------------------------------

type b6Child struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	idx     int
}

type b6ChildResult struct {
	idx     int
	output  []string
	exitErr error
	done    bool
	overlap bool
	refused bool
}

// TestDevinB6SerializationAcrossEightProcesses asserts that eight real
// processes can each add one distinct aggregate entry to the same canonical
// Devin root without losing any of them. Each child is a partial run (pruning
// disabled), so the only correct end state is the union of all eight entries.
//
// If the per-root read-modify-commit section is reverted, the children read the
// same empty aggregate and the last writer overwrites the earlier ones; entries
// disappear deterministically.
func TestDevinB6SerializationAcrossEightProcesses(t *testing.T) {
	if os.Getenv(b6ChildIndexEnv) != "" {
		t.Skip("this is the child-process body")
	}

	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	stateRoot := os.Getenv("XDG_STATE_HOME")
	witnessPath := filepath.Join(t.TempDir(), "section.witness")

	children := startB6Children(t, root, stateRoot, witnessPath, 8, "60s")
	results := releaseAndCollectB6Children(t, children)

	for _, r := range results {
		if r.overlap {
			t.Fatalf("child %d was inside the per-root section while another process was too; output:\n%s",
				r.idx, strings.Join(r.output, "\n"))
		}
		if r.exitErr != nil {
			t.Fatalf("child %d exited with an error: %v; output:\n%s", r.idx, r.exitErr, strings.Join(r.output, "\n"))
		}
		if !r.done {
			t.Fatalf("child %d did not finish; output:\n%s", r.idx, strings.Join(r.output, "\n"))
		}
	}

	if _, err := os.Lstat(witnessPath); !os.IsNotExist(err) {
		t.Fatalf("section witness survived: %v; a section was left open", err)
	}

	mcp := readFileString(t, filepath.Join(root, harness.DevinMcpConfigFile))
	hooks := readFileString(t, filepath.Join(root, harness.DevinProjectHooksFile))
	for i := 0; i < 4; i++ {
		if !strings.Contains(mcp, fmt.Sprintf(`"server-%d"`, i)) {
			t.Fatalf("server-%d missing from mcp_config.json:\n%s", i, mcp)
		}
		if !strings.Contains(hooks, fmt.Sprintf("hook-cmd-%d", i+4)) {
			t.Fatalf("hook %d missing from hooks.v1.json:\n%s", i+4, hooks)
		}
	}

	assertRecordDescribesDevinFile(t, 0, root, harness.DevinMcpConfigFile)
	assertRecordDescribesDevinFile(t, 0, root, harness.DevinProjectHooksFile)

	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if rec.Generation != 8 {
		t.Fatalf("record generation = %d, want 8", rec.Generation)
	}
}

// TestDevinB6ZeroLockTimeoutRefusesAllChildren proves that
// AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT=0 means fail fast: when the per-root
// lock is already held, every child is refused with ownership.lock-timeout and
// no aggregate file is written.
func TestDevinB6ZeroLockTimeoutRefusesAllChildren(t *testing.T) {
	if os.Getenv(b6ChildIndexEnv) != "" {
		t.Skip("this is the child-process body")
	}

	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	xdgStateHome := os.Getenv("XDG_STATE_HOME")
	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	witnessPath := filepath.Join(t.TempDir(), "section.witness")

	letGo := startDevinLockHolder(t, root, stateRoot)
	defer letGo()

	children := startB6Children(t, root, xdgStateHome, witnessPath, 8, "0")
	results := releaseAndCollectB6Children(t, children)

	for _, r := range results {
		if r.overlap {
			t.Fatalf("child %d reported an overlap while the lock was pre-held; output:\n%s",
				r.idx, strings.Join(r.output, "\n"))
		}
		if r.exitErr != nil {
			t.Fatalf("child %d exited with an error: %v; output:\n%s", r.idx, r.exitErr, strings.Join(r.output, "\n"))
		}
		if !r.refused {
			t.Fatalf("child %d was not refused with a zero timeout; output:\n%s", r.idx, strings.Join(r.output, "\n"))
		}
	}

	for _, rel := range []string{harness.DevinMcpConfigFile, harness.DevinProjectHooksFile} {
		if _, err := os.Lstat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Fatalf("zero-timeout refusal wrote %s: %v", rel, err)
		}
	}
}

func startB6Children(t *testing.T, root, stateRoot, witnessPath string, count int, timeout string) []*b6Child {
	t.Helper()
	var children []*b6Child
	for i := 0; i < count; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDevinB6SerializationChildProcess$", "-test.timeout=120s")
		cmd.Env = append(os.Environ(),
			b6ChildIndexEnv+"="+strconv.Itoa(i),
			b6ChildTargetEnv+"="+root,
			b6ChildStateEnv+"="+stateRoot,
			b6ChildWitnessEnv+"="+witnessPath,
			b6ChildTimeoutEnv+"="+timeout,
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
		children = append(children, &b6Child{cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout), idx: i})
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	for _, c := range children {
		if !scanFor(c.scanner, b6Ready) {
			t.Fatalf("child %d never announced readiness", c.idx)
		}
	}
	return children
}

func releaseAndCollectB6Children(t *testing.T, children []*b6Child) []b6ChildResult {
	t.Helper()
	for _, c := range children {
		if _, err := c.stdin.Write([]byte("\n")); err != nil {
			t.Errorf("child %d: release write: %v", c.idx, err)
		}
		_ = c.stdin.Close()
	}

	results := make([]b6ChildResult, len(children))
	for i, c := range children {
		results[i].idx = c.idx
		for c.scanner.Scan() {
			line := c.scanner.Text()
			results[i].output = append(results[i].output, line)
			switch {
			case strings.Contains(line, b6Overlap):
				results[i].overlap = true
			case strings.Contains(line, b6Refused):
				results[i].refused = true
			case strings.Contains(line, b6Done):
				results[i].done = true
			}
		}
		if err := c.scanner.Err(); err != nil {
			t.Errorf("child %d: read stdout: %v", c.idx, err)
		}
		if err := c.cmd.Wait(); err != nil {
			results[i].exitErr = err
		}
	}
	return results
}
