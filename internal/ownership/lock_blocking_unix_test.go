//go:build unix

package ownership

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidateLockFileRestoresBlockingIO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	fd, err := syscall.Open(path, lockOpenFlags, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), path)
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := validateLockFile(file); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("validated lock retained nonblocking I/O: flags=%#x", flags)
	}
}
