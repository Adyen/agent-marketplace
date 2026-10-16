package backup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestRestoreArchiveFIFORefusesWithoutBlocking(t *testing.T) {
	if os.Getenv("AM_BACKUP_FIFO_CHILD") == "1" {
		home, root := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		archiveDir := filepath.Join(home, ".agent-marketplace", "backups", "factory")
		if err := os.MkdirAll(archiveDir, 0o700); err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(archiveDir, "2026-10-03-120000.tar.gz")
		if err := syscall.Mkfifo(archive, 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(archive)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Restore(hcc.TargetFactory, root); !errors.Is(err, fsboundary.ErrUnsafeFile) {
			t.Fatalf("FIFO admission did not refuse: %v", err)
		}
		if after, err := os.Lstat(archive); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Fatalf("FIFO changed: %v", err)
		}
		if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
			t.Fatalf("FIFO refusal published target files: %v %v", entries, err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRestoreArchiveFIFORefusesWithoutBlocking$", "-test.count=1")
	command.Env = append(os.Environ(), "AM_BACKUP_FIFO_CHILD=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("bounded real restore FIFO probe failed: %v %s", err, output)
	}
}
