package dependencytxn

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestReadManagedLockPreservesReaderErrorIdentities(t *testing.T) {
	_, agents, state := repoLayer(t)
	valid := filepath.Join(agents, "lock.yaml")
	mustWrite(t, valid, []byte("apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries: []\n"), 0o600)
	if data, generation, _, err := ReadSnapshot(valid); err != nil || !generation.Exists || len(data) == 0 {
		t.Fatalf("valid lock control failed: %v", err)
	}

	fifo := filepath.Join(agents, "lock.local.yaml")
	if err := os.Remove(fifo); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ReadSnapshot(fifo); !errors.Is(err, lockfile.ErrUnsafeFile) {
		t.Fatalf("FIFO lock error = %v, want lockfile.ErrUnsafeFile", err)
	}

	large := filepath.Join(agents, "lock.yaml")
	if err := os.Truncate(large, maxDependencyFileSize+1); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ReadSnapshot(large); !errors.Is(err, lockfile.ErrFileTooLarge) {
		t.Fatalf("oversized lock error = %v, want lockfile.ErrFileTooLarge", err)
	}

	if _, err := Commit(t.Context(), state, []DesiredFile{{Path: fifo, Bytes: []byte("write"), Mode: 0o600}}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("FIFO publication error = %v, want transaction ErrUnsafePath", err)
	}
}
