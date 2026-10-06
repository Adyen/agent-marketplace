package ownership

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
)

func TestOwnershipMetadataFIFORefusesWithoutBlocking(t *testing.T) {
	if os.Getenv("AM_OWNERSHIP_METADATA_FIFO_CHILD") == "1" {
		for _, kind := range []string{"state", "journal"} {
			f := newFixture(t)
			store := f.store()
			if err := os.MkdirAll(store.recordDir(), 0o700); err != nil {
				t.Fatal(err)
			}
			path := store.StateFile()
			if kind == "journal" {
				path = store.JournalFile()
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "state" {
				_, err = store.Load()
			} else {
				_, err = store.ReadJournal()
			}
			if !errors.Is(err, fsboundary.ErrUnsafeFile) {
				t.Fatalf("FIFO metadata admitted: %v", err)
			}
			if after, err := os.Lstat(path); err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
				t.Fatalf("FIFO changed: %v", err)
			}
			if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 1 {
				t.Fatalf("read-only FIFO probe created residue: %v %v", entries, err)
			}
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnershipMetadataFIFORefusesWithoutBlocking$", "-test.count=1")
	command.Env = append(os.Environ(), "AM_OWNERSHIP_METADATA_FIFO_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bounded metadata FIFO probe failed: %v %s", err, output)
	}
}
