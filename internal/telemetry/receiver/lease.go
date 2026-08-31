package receiver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type storageLease struct {
	mu   sync.Mutex
	file *os.File
}

func acquireStorageLease(dir string) (*storageLease, error) {
	path := filepath.Join(dir, ".receiver.lock")
	file, err := os.OpenFile(path, leaseOpenFlags, recordFilePerm)
	if err != nil {
		return nil, fmt.Errorf("open receiver storage lock: %w", err)
	}
	if err := tryLeaseFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock receiver storage directory %q: %w", dir, err)
	}
	return &storageLease{file: file}, nil
}

func (l *storageLease) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := errors.Join(unlockLeaseFile(l.file), l.file.Close())
	l.file = nil
	return err
}
