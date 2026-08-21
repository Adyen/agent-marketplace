//go:build unix

package cache

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"syscall"
)

// Written against the standard library's syscall package rather than
// golang.org/x/sys/unix for the same reason internal/ownership is: x/sys is only
// an indirect dependency of the charm stack, and importing it here would promote
// it into the direct require block, which `go mod tidy -diff` flags in CI.
// syscall.Flock is the same syscall with a coarser errno type.

// lockDir takes a blocking exclusive advisory lock on an open directory
// descriptor.
//
// flock(2) is owned by the open file description, so the kernel releases the
// lock when the descriptor closes — including when the holder crashes. That is
// why there is no lock timeout and no stale-lock heuristic to get wrong.
//
// Blocking is deliberate: a waiter wants the other process's clone to finish and
// then to observe its result, not to fail or to clone the same ref twice. The
// non-blocking attempt first is only so that contention is visible in the log.
func lockDir(dir *os.File) error {
	err := flockDir(dir, syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return nil
	}
	if !errors.Is(err, errLockBusy) {
		return err
	}
	slog.Debug("waiting for exclusive registry cache lock", "dir", dir.Name())
	err = flockDir(dir, syscall.LOCK_EX)
	if errors.Is(err, errLockBusy) {
		// A *blocking* flock reporting contention means ENOLCK: the kernel's lock
		// table is exhausted, not that someone holds the lock. There is nothing
		// to wait for, so degrade instead of failing the sync.
		return errors.Join(errLockUnsupported, err)
	}
	return err
}

func unlockDir(dir *os.File) error {
	return flockDir(dir, syscall.LOCK_UN)
}

func flockDir(dir *os.File, how int) error {
	// SyscallConn rather than Fd(): Fd() puts the descriptor into blocking mode
	// and detaches it from the runtime poller as a side effect, and neither is
	// wanted for a descriptor that is also handed to subprocesses.
	raw, err := dir.SyscallConn()
	if err != nil {
		return fmt.Errorf("raw directory handle: %w", err)
	}
	var opErr error
	if ctlErr := raw.Control(func(fd uintptr) {
		for {
			opErr = syscall.Flock(int(fd), how)
			// flock is interruptible by a signal, and a Go program takes signals
			// (SIGURG for preemption) constantly, so EINTR is routine rather than
			// a lock failure.
			if !errors.Is(opErr, syscall.EINTR) {
				return
			}
		}
	}); ctlErr != nil {
		return fmt.Errorf("raw directory control: %w", ctlErr)
	}
	return classifyLockError(opErr)
}

func classifyLockError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.ENOLCK):
		// EWOULDBLOCK and EAGAIN are the same value on darwin and linux; both are
		// spelled out so the intent survives a platform where they are not.
		// ENOLCK is transient lock-table exhaustion, not a filesystem capability.
		return errors.Join(errLockBusy, err)
	case errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP):
		// The realistic case is a cache on a filesystem that refuses advisory
		// locks on directories (some network mounts). Degrade rather than refuse
		// to sync: publication is a rename that converges, so an unserialized
		// run is correct, just wasteful.
		return errors.Join(errLockUnsupported, err)
	default:
		return err
	}
}
