package ownership

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestLockContextFIFORefusesWithoutBlocking(t *testing.T) {
	if os.Getenv("AM_LOCK_FIFO_CHILD") == "1" {
		store := newFixture(t).store()
		if err := os.MkdirAll(filepath.Dir(store.LockPath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(store.LockPath(), 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(store.LockPath())
		if err != nil {
			t.Fatal(err)
		}
		release, err := store.LockContext(context.Background())
		if release != nil {
			release()
		}
		if !errors.Is(err, fsboundary.ErrUnsafeFile) {
			t.Fatalf("FIFO admitted as ownership lock: %v", err)
		}
		if !strings.Contains(err.Error(), store.LockPath()) {
			t.Fatalf("FIFO lock refusal does not identify the lock path: %v", err)
		}
		if after, err := os.Lstat(store.LockPath()); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Fatalf("FIFO lock changed: %v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockContextFIFORefusesWithoutBlocking$", "-test.count=1")
	command.Env = append(os.Environ(), "AM_LOCK_FIFO_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bounded lock FIFO witness failed: %v %s", err, output)
	}
}
