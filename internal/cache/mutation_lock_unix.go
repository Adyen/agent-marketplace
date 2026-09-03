//go:build unix

package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	lockfileShared    = unix.LOCK_SH
	lockfileExclusive = unix.LOCK_EX
)

func acquireCacheFileLock(operation int) (func(), error) {
	return acquireCacheFileLockWithContext(context.Background(), operation)
}

func acquireCacheFileLockWithContext(ctx context.Context, operation int) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cacheRoot := CacheDir()
	lockPath := cacheRoot + ".mutation.lock"
	parent := filepath.Dir(cacheRoot)
	if err := ValidateDirectoryAncestry(filepath.Dir(parent), parent); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("validate cache lock parent: %w", err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("create cache lock parent: %w", err)
	}
	if err := ValidateDirectoryAncestry(filepath.Dir(parent), parent); err != nil {
		return nil, fmt.Errorf("validate cache lock parent: %w", err)
	}

	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cache mutation lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), lockPath)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect cache mutation lock: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = file.Close()
		return nil, errors.New("cache mutation lock is not a regular file")
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure cache mutation lock: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		err := unix.Flock(fd, operation|unix.LOCK_NB)
		if err == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				_ = unix.Flock(fd, unix.LOCK_UN)
				_ = file.Close()
				return nil, ctxErr
			}
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			_ = file.Close()
			return nil, fmt.Errorf("lock cache mutation: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = unix.Flock(fd, unix.LOCK_UN)
			_ = file.Close()
		})
	}, nil
}
