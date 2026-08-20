//go:build unix

package config

import (
	"errors"
	"os"
	"syscall"
)

const migrationLockOpenFlags = os.O_CREATE | os.O_RDWR | syscall.O_NOFOLLOW

func lockMigrationFile(file *os.File) error {
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

func unlockMigrationFile(file *os.File) error {
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
