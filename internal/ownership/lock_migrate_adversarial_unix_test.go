//go:build unix

package ownership

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// ---------------------------------------------------------------------------
// File-descriptor exhaustion must REFUSE, never degrade
// ---------------------------------------------------------------------------

// This is the highest-value attack on the degrade branch, because it is the one
// an unlucky machine reaches without any attacker at all.
//
// Acquisition opens locks/<rootKey>.lock before it calls flock. Under EMFILE that
// open fails — and if that failure were classified as "this filesystem does not
// support advisory locking", every writer on a machine that ran out of
// descriptors would silently proceed UNSERIALIZED, which is precisely the P0
// defect the parent bead exists to remove, reintroduced by a resource limit
// instead of by a race.
//
// It must therefore be a LOUD refusal: an error that does not wrap
// ErrLockUnsupported, no ownership.lock-unavailable warning, and no mutation.
//
// The exhaustion is real, not simulated: the child lowers its own
// RLIMIT_NOFILE and then opens descriptors until the kernel refuses. It runs in a
// child process because an exhausted descriptor table is not something to inflict
// on the rest of a test binary, and because Setrlimit is process-wide.

const (
	fdChildModeEnv  = "AM_OWNERSHIP_FD_CHILD_MODE"
	fdChildRootEnv  = "AM_OWNERSHIP_FD_CHILD_TARGET_ROOT"
	fdChildStateEnv = "AM_OWNERSHIP_FD_CHILD_STATE_ROOT"

	// The child reports one of these on stdout. Anything else is a bug in the
	// child itself and fails the parent.
	fdVerdictRefused    = "VERDICT refused-loudly"
	fdVerdictDegraded   = "VERDICT degraded-unserialized"
	fdVerdictProceeded  = "VERDICT proceeded"
	fdVerdictNoPressure = "VERDICT no-pressure"
)

// TestFdExhaustionChildProcess is not a test: it is the body of the child the
// parent below execs, inert unless its mode env var is set.
func TestFdExhaustionChildProcess(t *testing.T) {
	if os.Getenv(fdChildModeEnv) == "" {
		t.Skip("not a child process invocation")
	}
	targetRoot := os.Getenv(fdChildRootEnv)
	rel := "mcp_config.json"
	abs := filepath.Join(targetRoot, rel)

	st, err := Open(targetRoot, WithStateRoot(os.Getenv(fdChildStateEnv)), WithLockTimeout(0))
	if err != nil {
		fmt.Println("VERDICT open-failed", err)
		return
	}

	// Everything the mutation needs is prepared BEFORE the descriptor table is
	// exhausted, so the failure under test is the LOCK's open and not the
	// fixture's own reads.
	content := map[string]string{"alpha": aggregateValue("alpha")}
	proof, _ := st.Proof(rel)
	mutation := Mutation{
		Rel:                   rel,
		AbsFile:               abs,
		NextBytes:             []byte("{\n  \"alpha\": \"value-alpha\"\n}\n"),
		Entries:               aggregateEntries(content),
		Reason:                ReasonSync,
		ExpectPriorGeneration: proof.Generation(),
		ExpectPriorDigest:     "",
	}
	hogDir := os.Getenv(fdChildStateEnv) + ".hog"
	if err := os.MkdirAll(hogDir, 0o755); err != nil {
		fmt.Println("VERDICT hogdir-failed", err)
		return
	}
	hogPath := filepath.Join(hogDir, "hog")
	if err := os.WriteFile(hogPath, nil, 0o600); err != nil {
		fmt.Println("VERDICT hogfile-failed", err)
		return
	}

	// Lower the soft limit so exhaustion costs a few dozen descriptors rather
	// than a few thousand. Descriptors already open stay open.
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err == nil {
		lowered := limit
		lowered.Cur = 64
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lowered)
		defer func() { _ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit) }()
	}

	var hogs []*os.File
	exhausted := false
	for i := 0; i < 100000; i++ {
		f, openErr := os.OpenFile(hogPath, os.O_RDONLY, 0)
		if openErr != nil {
			exhausted = true
			break
		}
		hogs = append(hogs, f)
	}
	_, applyErr := st.Apply(mutation)
	for _, f := range hogs {
		_ = f.Close()
	}

	// Only now, with descriptors available again, is it safe to report.
	switch {
	case !exhausted:
		fmt.Println(fdVerdictNoPressure)
	case applyErr == nil:
		fmt.Println(fdVerdictProceeded)
	case errors.Is(applyErr, ErrLockUnsupported):
		fmt.Println(fdVerdictDegraded, applyErr)
	default:
		fmt.Println(fdVerdictRefused, applyErr)
	}
	// Whether a record exists is the ground truth for "did it mutate anyway".
	fmt.Println("STATE-EXISTS", fileExistsPlain(st.StateFile()))
	fmt.Println("HOGS", len(hogs))
}

func fileExistsPlain(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// TestAdversarialFdExhaustionRefusesRatherThanSilentlyDeserializing runs the
// child above and requires the loud-refusal verdict.
//
// A "degraded" verdict here would be a silent de-serialization severity finding:
// on a machine under descriptor pressure the P0 fix would switch itself off with
// nothing but a warning that says the filesystem cannot lock, which would be a
// lie.
func TestAdversarialFdExhaustionRefusesRatherThanSilentlyDeserializing(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	cmd := exec.Command(os.Args[0], "-test.run=^TestFdExhaustionChildProcess$", "-test.timeout=120s")
	cmd.Env = append(os.Environ(),
		fdChildModeEnv+"=1",
		fdChildRootEnv+"="+f.targetRoot,
		fdChildStateEnv+"="+f.stateRoot,
	)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var lines []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("fd-exhaustion child: %v\n%s", err, strings.Join(lines, "\n"))
	}
	out := strings.Join(lines, "\n")
	// Logged unconditionally: the verdict and the number of descriptors the child
	// actually managed to hog are the evidence that this test is not vacuous.
	t.Logf("fd-exhaustion child:\n%s", out)

	switch {
	case strings.Contains(out, fdVerdictNoPressure):
		t.Skipf("could not exhaust the descriptor table on this machine, so nothing was proven:\n%s", out)
	case strings.Contains(out, fdVerdictDegraded):
		t.Fatalf("file-descriptor exhaustion took the ErrLockUnsupported branch, so the mutation ran with NO serialization "+
			"and reported 'this filesystem does not support advisory locking'. That is a silent de-serialization "+
			"reachable without an attacker:\n%s", out)
	case strings.Contains(out, fdVerdictProceeded):
		t.Fatalf("the mutation succeeded despite being unable to open its lock file:\n%s", out)
	case strings.Contains(out, fdVerdictRefused):
		// The required outcome. Confirm it wrote nothing.
		if strings.Contains(out, "STATE-EXISTS true") {
			t.Fatalf("a refused mutation committed a record anyway:\n%s", out)
		}
	default:
		t.Fatalf("the child reported no verdict:\n%s", out)
	}
	if fileExists(t, f.store().StateFile()) {
		t.Fatal("the child committed an ownership record while unable to lock")
	}
	if content, _ := f.readAggregate(); content != "{}\n" {
		t.Fatalf("the child mutated the aggregate while unable to lock: %q", content)
	}
}

// ---------------------------------------------------------------------------
// The degrade set is exactly the design's, per writer entry point
// ---------------------------------------------------------------------------

// TestAdversarialOnlyTheDocumentedErrnosDegradeAWriter is the regression guard on
// the one constant that decides whether the P0 fix is on.
//
// classifyFlockError's degrade set is the design's "Filesystems that refuse to
// lock" set. Anything added to it becomes a new way to run a mutation
// unserialized, and the additions a future maintainer is most likely to reach for
// — EINVAL (a bug in this package), EACCES/EPERM (a permission problem on the
// state root), EIO, EBADF — are each pinned here as REFUSALS at the Apply level,
// not merely at classifyFlockError's, so the guard covers the whole path from
// errno to mutation.
//
// ENOLCK is in the degrade set and is asserted as degrading, because that is the
// design's decision. It is also the weakest member of the set: on Linux flock can
// return ENOLCK when the kernel's lock table is exhausted, which is a resource
// condition rather than a filesystem capability. See the review notes on bead
// agent-marketplace-mfez; narrowing it belongs with B5's compare-and-swap
// backstop, which is what makes the degraded path safe at all.
func TestAdversarialOnlyTheDocumentedErrnosDegradeAWriter(t *testing.T) {
	for _, tc := range []struct {
		errno       syscall.Errno
		wantDegrade bool
	}{
		{syscall.ENOTSUP, true},
		{syscall.EOPNOTSUPP, true},
		// ENOLCK is retried like contention; with a zero timeout it degrades in
		// one attempt instead of waiting for the default 30 s deadline.
		{syscall.ENOLCK, true},
		{syscall.EINVAL, false},
		{syscall.EACCES, false},
		{syscall.EPERM, false},
		{syscall.EIO, false},
		{syscall.EBADF, false},
		{syscall.ENOSPC, false},
	} {
		t.Run(tc.errno.Error(), func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate("{}\n")
			logs := captureLogs(t)
			opts := []Option{}
			if tc.errno == syscall.ENOLCK {
				opts = append(opts, WithLockTimeout(0))
			}
			st := f.store(opts...)
			errno := tc.errno
			st.tryLock = func(*os.File) error { return classifyFlockError(errno) }

			_, err := applyAddingKey(st, f.rel, f.absFile, "alpha")
			degraded := err == nil && strings.Contains(logs(), "NOT serialized")
			if degraded != tc.wantDegrade {
				t.Fatalf("errno %v: degraded=%v (err %v), want degraded=%v.\n"+
					"Widening the degrade set is a new way to run an ownership mutation with no serialization at all.",
					errno, degraded, err, tc.wantDegrade)
			}
			if !tc.wantDegrade {
				if err == nil {
					t.Fatalf("errno %v: the mutation proceeded", errno)
				}
				if errors.Is(err, ErrLockUnsupported) {
					t.Fatalf("errno %v was reported as 'this filesystem cannot lock': %v", errno, err)
				}
				if fileExists(t, st.StateFile()) {
					t.Fatalf("errno %v: a refused mutation committed a record", errno)
				}
				if content, _ := f.readAggregate(); content != "{}\n" {
					t.Fatalf("errno %v: a refused mutation rewrote the aggregate: %q", errno, content)
				}
			}
		})
	}
}
