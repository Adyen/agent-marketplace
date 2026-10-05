//go:build unix

package fsboundary

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOpenPersistentFileAdmitsOnlySingleRegularLeaves(t *testing.T) {
	t.Run("created leaf is admitted with blocking I/O", func(t *testing.T) {
		root, path := tempRoot(t)
		file, err := root.OpenPersistentFile("lock", 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		raw, err := file.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var flags int
		var flagsErr error
		if err := raw.Control(func(fd uintptr) { flags, flagsErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil || flagsErr != nil {
			t.Fatalf("inspect descriptor flags: %v %v", err, flagsErr)
		}
		if flags&unix.O_NONBLOCK != 0 {
			t.Fatal("admitted persistent file kept nonblocking I/O")
		}
		if info, err := os.Lstat(filepath.Join(path, "lock")); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("persistent file not created as regular: %v", err)
		}
	})
	t.Run("FIFO refuses without blocking", func(t *testing.T) {
		root, path := tempRoot(t)
		if err := unix.Mkfifo(filepath.Join(path, "lock"), 0o600); err != nil {
			t.Fatal(err)
		}
		if file, err := root.OpenPersistentFile("lock", 0o644); !errors.Is(err, ErrUnsafeFile) {
			if file != nil {
				_ = file.Close()
			}
			t.Fatalf("FIFO admitted as persistent file: %v", err)
		}
	})
	t.Run("hardlinked leaf refuses", func(t *testing.T) {
		root, path := tempRoot(t)
		leaf := filepath.Join(path, "lock")
		mustWrite(t, leaf, []byte("inert lock metadata"), 0o644)
		if err := os.Link(leaf, filepath.Join(path, "foreign")); err != nil {
			t.Fatal(err)
		}
		if file, err := root.OpenPersistentFile("lock", 0o644); !errors.Is(err, ErrUnsafeFile) {
			if file != nil {
				_ = file.Close()
			}
			t.Fatalf("hardlinked leaf admitted as persistent file: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(path, "foreign")); err != nil || string(data) != "inert lock metadata" {
			t.Fatalf("refusal changed linked bytes: %q %v", data, err)
		}
	})
}
