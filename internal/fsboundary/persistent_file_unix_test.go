//go:build unix

package fsboundary

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOpenPersistentFileRestoresBlockingIO(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := t.TempDir()
			if existing {
				if err := os.WriteFile(filepath.Join(path, "lock"), []byte("inert lock"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			root, err := OpenDir(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			})
			file, err := root.OpenPersistentFile("lock", 0o600)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := file.Close(); err != nil {
					t.Error(err)
				}
			})
			conn, err := file.SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			var flags int
			var controlErr error
			if err := conn.Control(func(fd uintptr) {
				flags, controlErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
			}); err != nil {
				t.Fatal(err)
			}
			if controlErr != nil {
				t.Fatal(controlErr)
			}
			if flags&unix.O_NONBLOCK != 0 {
				t.Fatalf("admitted persistent file retained nonblocking I/O: flags=%#x", flags)
			}
		})
	}
}
