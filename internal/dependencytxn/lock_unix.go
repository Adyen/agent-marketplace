//go:build unix

package dependencytxn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type heldLock struct {
	file *os.File
}

func acquireLocks(ctx context.Context, stateDir string, paths []string) ([]heldLock, error) {
	lockDir, err := prepareStateDir(filepath.Join(stateDir, "locks"))
	if err != nil {
		return nil, err
	}
	locks := make([]heldLock, 0, len(paths))
	for _, path := range paths {
		lock, err := acquireLock(ctx, lockPath(lockDir, path))
		if err != nil {
			releaseLocks(locks)
			return nil, err
		}
		locks = append(locks, lock)
	}
	return locks, nil
}

func acquireLock(ctx context.Context, path string) (heldLock, error) {
	if err := ctx.Err(); err != nil {
		return heldLock{}, errors.Join(ErrLockTimeout, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return heldLock{}, fmt.Errorf("dependency transaction: open lock %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err == nil {
			err = ErrUnsafePath
		}
		return heldLock{}, fmt.Errorf("dependency transaction: inspect lock %s: %w", path, err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return heldLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			_ = file.Close()
			return heldLock{}, fmt.Errorf("dependency transaction: lock %s: %w", path, err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return heldLock{}, errors.Join(ErrLockTimeout, ctx.Err())
		case <-timer.C:
		}
	}
}

func releaseLocks(locks []heldLock) {
	for i := len(locks) - 1; i >= 0; i-- {
		_ = syscall.Flock(int(locks[i].file.Fd()), syscall.LOCK_UN)
		_ = locks[i].file.Close()
	}
}

func lockPath(lockDir, path string) string {
	return filepath.Join(lockDir, "sha256-"+Digest([]byte(path))+".lock")
}
