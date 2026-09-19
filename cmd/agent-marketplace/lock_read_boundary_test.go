package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestStatusRejectsFIFOLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testReadOnlyCommandFIFOLock(t, "status")
}

func TestGCRejectsFIFOLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testReadOnlyCommandFIFOLock(t, "gc", "--dry-run")
}

func testReadOnlyCommandFIFOLock(t *testing.T, args ...string) {
	t.Helper()
	if os.Getenv("LOCK_FIFO_CHILD") != "1" {
		ready := filepath.Join(t.TempDir(), "ready")
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		result := runCommandProcess(ctx, t, t.Name(), exactCommandTestPattern(t.Name()), "LOCK_FIFO_CHILD=1", "LOCK_FIFO_READY="+ready)
		raw, err := os.ReadFile(ready)
		if err != nil || string(raw) != "ready" {
			t.Fatalf("did not reach FIFO lock command: %q %v; %s", raw, err, result.output)
		}
		if result.err != nil {
			t.Fatalf("FIFO lock command did not refuse promptly: %v\n%s", result.err, result.output)
		}
		return
	}
	f := newManagedSkillLinkFixture(t, "factory", "repo")
	if out, err := executeCommand(args...); err != nil {
		t.Fatalf("valid-lock control: %v\n%s", err, out)
	}
	path := filepath.Join(f.repo, ".agents", "lock.yaml")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := ownedCommandSnapshot(t, f.base, path)
	if err := os.WriteFile(os.Getenv("LOCK_FIFO_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := executeCommand(args...)
	if !errors.Is(err, lockfile.ErrUnsafeFile) {
		t.Fatalf("error=%v output=%s", err, out)
	}
	afterInfo, err := os.Lstat(path)
	if err != nil || afterInfo.Mode()&os.ModeNamedPipe == 0 || !os.SameFile(info, afterInfo) {
		t.Fatalf("FIFO changed: %v", err)
	}
	if after := ownedCommandSnapshot(t, f.base, path); !reflect.DeepEqual(before, after) {
		for path, value := range before {
			if after[path] != value {
				t.Errorf("refusal changed or removed %s", path)
			}
		}
		for path := range after {
			if _, exists := before[path]; !exists {
				t.Errorf("refusal created %s", path)
			}
		}
	}
}
