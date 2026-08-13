//go:build unix

package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// TestAdversarialLockDescriptorIsCloseOnExec attacks the lifetime of the lock
// from the one direction the SIGKILL test cannot reach. A flock belongs to the
// OPEN FILE DESCRIPTION, and a fork+exec that inherits the descriptor inherits
// the description — so a subprocess spawned while the lock is held (this CLI
// spawns git) would keep the lock alive after its parent died, manufacturing
// exactly the stale lock the package's doc comment says cannot exist.
//
// The whole no-stale-lock story therefore rests on FD_CLOEXEC, which the os
// package sets for us. That is an assumption worth pinning rather than
// inheriting.
func TestAdversarialLockDescriptorIsCloseOnExec(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	release, err := st.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer release()

	st.lockMu.Lock()
	file := st.session.f
	st.lockMu.Unlock()

	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags uintptr
	var errno syscall.Errno
	if ctlErr := raw.Control(func(fd uintptr) {
		flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, uintptr(syscall.F_GETFD), 0)
	}); ctlErr != nil {
		t.Fatal(ctlErr)
	}
	if errno != 0 {
		t.Fatal(errno)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("the lock descriptor is not close-on-exec: any subprocess spawned inside a critical section " +
			"would inherit the open file description and keep the lock alive after the holder died")
	}
}

// TestAdversarialUnlinkingTheLockFileSplitsTheLock is a CHARACTERIZATION test of
// the hazard lock.go's "NEVER unlink the lock file" rule exists to prevent. It
// is not a defect in this package — nothing here unlinks — but the rule is only
// as strong as the reasons for it, and a reason that is written down but never
// demonstrated is a reason a future `gc` implementation will talk itself out of.
//
// With the file unlinked out from under a holder, a second Store creates a new
// inode and both hold "the" lock at the same time.
func TestAdversarialUnlinkingTheLockFileSplitsTheLock(t *testing.T) {
	f := newFixture(t)
	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer release()

	// Sanity: exclusive while the file is where it belongs. Without this the
	// assertion below would also pass on a tree with no locking at all.
	if _, err := f.store(WithLockTimeout(0)).Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("the lock was not exclusive to begin with: %v", err)
	}

	if err := os.Remove(holder.LockPath()); err != nil {
		t.Fatal(err)
	}
	second, err := f.store(WithLockTimeout(0)).Lock()
	if err != nil {
		t.Fatalf("unlinking the lock file no longer splits the lock (%v) — if that is because acquisition now "+
			"pins the inode, delete this test and say so in lock.go", err)
	}
	second()
	t.Log("confirmed: unlinking the lock file gives two simultaneous holders; this is why nothing may reap locks/")
}

// TestAdversarialLockPathThroughASymlinkDoesNotTruncateTheVictim: acquisition
// opens the lock path with O_CREATE|O_RDWR and then TRUNCATES it to write its
// diagnostics-only metadata. If the final path component is a symlink, that
// truncation lands on whatever the symlink names — including this package's own
// ownership record. Advisory locking must never be able to destroy a file it
// does not own, so the symlink must be refused rather than followed.
func TestAdversarialLockPathThroughASymlinkDoesNotTruncateTheVictim(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(filepath.Dir(st.LockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "precious.json")
	const contents = `{"version":1,"files":{}}`
	if err := os.WriteFile(victim, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, st.LockPath()); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	release, err := st.Lock()
	if err == nil {
		release()
	}
	got, readErr := os.ReadFile(victim)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != contents {
		t.Fatalf("acquiring the lock rewrote a file the lock path pointed at through a symlink: %q", got)
	}
	if err == nil {
		t.Fatal("acquisition through a symlinked lock path succeeded; it must be refused loudly")
	}
	if errors.Is(err, ErrLockTimeout) || errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("a symlinked lock path was reported as %v, which a caller would degrade or retry on", err)
	}
	if st.HoldsLock() {
		t.Fatal("the store believes it holds a lock it never took")
	}
}

// TestAdversarialUnsupportedFilesystemDegradesWithoutHoldingAnything exercises
// the branch no test on a supported platform can otherwise reach: the whole
// ErrLockUnsupported path is dead code on darwin and linux (it needs an
// NFS-mounted state root) and is the ONLY behaviour on GOOS=windows, where the
// stub returns it unconditionally. B2 and B3 are about to write callers for it,
// so its exact contract is pinned here.
func TestAdversarialUnsupportedFilesystemDegradesWithoutHoldingAnything(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	// The classifier, not the test, decides what counts as unsupported: reusing
	// it means this test cannot drift from filelock_unix.go's errno set.
	st.tryLock = func(*os.File) error { return classifyFlockError(syscall.ENOTSUP) }

	var kinds []LockEventKind
	st.lockObserver = func(ev LockEvent) { kinds = append(kinds, ev.Kind) }

	release, err := st.Lock()
	if err == nil {
		release()
		t.Fatal("acquisition reported success on a filesystem that refuses advisory locks")
	}
	if !errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("degradation must be recognizable as ErrLockUnsupported, got %v", err)
	}
	if errors.Is(err, ErrLockTimeout) {
		t.Fatal("an unsupported filesystem must not be mistakable for a timeout: the responses are opposite")
	}
	if release != nil {
		t.Fatal("the degraded path returned a non-nil releaser with an error")
	}
	if st.HoldsLock() {
		t.Fatal("the store believes it holds a lock on a filesystem that cannot lock")
	}
	var ce *diag.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("the degraded error carries no diagnostic code: %v", err)
	}
	if ce.Code != diag.CodeOwnershipLockUnavailable {
		t.Fatalf("degraded error code = %q, want %q", ce.Code, diag.CodeOwnershipLockUnavailable)
	}
	if ce.Severity != diag.SeverityWarning {
		t.Fatalf("degraded error severity = %v, want %v", ce.Severity, diag.SeverityWarning)
	}
	found := false
	for _, k := range kinds {
		if k == LockUnsupported {
			found = true
		}
		if k == LockAcquired {
			t.Fatal("the observer saw an acquisition on a filesystem that cannot lock")
		}
	}
	if !found {
		t.Fatalf("observed events %v contain no %q", kinds, LockUnsupported)
	}

	// And the degradation is not sticky: a Store on a working filesystem is
	// unaffected.
	working := f.store(WithLockTimeout(0))
	releaseWorking, err := working.Lock()
	if err != nil {
		t.Fatalf("a healthy store could not acquire after a degraded one: %v", err)
	}
	releaseWorking()
}
