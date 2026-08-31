//go:build unix

package receiver

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

const leaseOpenFlags = os.O_CREATE | os.O_RDWR | syscall.O_NOFOLLOW

func tryLeaseFile(file *os.File) error {
	if err := leaseFlock(file, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf("storage directory is already in use: %w", err)
		}
		return err
	}
	return nil
}

func unlockLeaseFile(file *os.File) error {
	return leaseFlock(file, syscall.LOCK_UN)
}

func leaseFlock(file *os.File, operation int) error {
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var operationErr error
	if err := raw.Control(func(fd uintptr) {
		for {
			operationErr = syscall.Flock(int(fd), operation)
			if !errors.Is(operationErr, syscall.EINTR) {
				return
			}
		}
	}); err != nil {
		return err
	}
	return operationErr
}
