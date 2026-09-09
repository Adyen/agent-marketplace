//go:build unix

package ownership

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// The unix half of the advisory lock. It is deliberately written against the
// standard library's syscall package rather than golang.org/x/sys/unix. The
// repository now uses x/sys directly for other filesystem operations, but
// x/sys/unix.Flock would still buy nothing here beyond a nicer errno type.
// syscall.Flock is also a pure syscall, so CGO_ENABLED=0 is unaffected.

// lockOpenFlags is how the lock file is opened. O_NOFOLLOW refuses a symlink in
// the final component: acquisition truncates this file to write its metadata,
// and a symlink would aim that truncation at a file this package does not own.
// A real lock file is always a regular file this package created.
const lockOpenFlags = os.O_CREATE | os.O_RDWR | syscall.O_NOFOLLOW

// tryLockFile takes an exclusive advisory lock on f WITHOUT blocking.
//
// It returns nil on success, [errLockBusy] when another open file description
// holds the lock (or when the kernel's lock table is temporarily exhausted,
// ENOLCK), [ErrLockUnsupported] when the filesystem refuses advisory locks, and
// the raw error otherwise.
//
// Non-blocking is not an optimization, it is the design: a blocking flock cannot
// be cancelled, so past a deadline its descriptor would leak and could later
// acquire a lock nobody is waiting on. See the comment block in lock.go.
func tryLockFile(f *os.File) error {
	return flockOp(f, syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlockFile drops the advisory lock. Closing the descriptor drops it too — the
// kernel owns that guarantee, which is why there is no stale-lock policy — so
// this is only for the case where a dup of the descriptor outlives the Close.
func unlockFile(f *os.File) error {
	return flockOp(f, syscall.LOCK_UN)
}

func flockOp(f *os.File, how int) error {
	// SyscallConn rather than Fd(): Fd() puts the file into blocking mode and
	// detaches it from the runtime poller as a side effect, and we need neither.
	raw, err := f.SyscallConn()
	if err != nil {
		return fmt.Errorf("ownership: raw file handle: %w", err)
	}
	var opErr error
	if ctlErr := raw.Control(func(fd uintptr) {
		for {
			opErr = syscall.Flock(int(fd), how)
			// flock is interruptible by a signal; a Go program takes signals
			// (SIGURG for preemption) constantly, so EINTR is routine and is not
			// a lock failure.
			if !errors.Is(opErr, syscall.EINTR) {
				return
			}
		}
	}); ctlErr != nil {
		return fmt.Errorf("ownership: raw file control: %w", ctlErr)
	}
	return classifyFlockError(opErr)
}

// isENOLCK reports whether err contains a syscall.ENOLCK errno, so the
// cross-platform acquisition loop can degrade only at the deadline without
// importing syscall in lock.go.
func isENOLCK(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == syscall.ENOLCK
}

// classifyFlockError maps the errnos flock can return onto this package's
// portable vocabulary.
func classifyFlockError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EAGAIN):
		// EWOULDBLOCK and EAGAIN are the same value on darwin and linux; both are
		// spelled out so the intent survives a platform where they are not.
		return errors.Join(errLockBusy, err)
	case errors.Is(err, syscall.ENOLCK):
		// ENOLCK is transient lock-table exhaustion on Linux, not a filesystem
		// capability. Retry it like contention; only degrade if it persists to
		// the lock timeout.
		return errors.Join(errLockBusy, err)
	case errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP):
		// The realistic case is an NFS-mounted state root on macOS. Degrading is
		// byte-for-byte today's behaviour on exactly the filesystems where
		// today's behaviour is unavoidable; the caller reports
		// ownership.lock-unavailable so the degradation is documented rather
		// than silent.
		//
		// This set is exactly the one the design fixes (section "Filesystems
		// that refuse to lock"); do not widen it. EINVAL in particular must NOT
		// be here: flock(2) returns it for an invalid operation, and `how` is a
		// compile-time constant, so an EINVAL is a bug in this package. Mapping
		// a bug onto "this filesystem cannot lock" would silently disable
		// serialization instead of reporting the bug.
		return errors.Join(ErrLockUnsupported, err)
	default:
		// Permission denied on the state root, ENOSPC, and friends are hard
		// failures: they are the same conditions under which the mandatory
		// safety backup would fail, and Apply already refuses a mutation whose
		// backup failed.
		return err
	}
}
