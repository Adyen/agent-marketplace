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

func TestSafetyPayloadFIFORefusesWithoutBlocking(t *testing.T) {
	if os.Getenv("AM_SAFETY_PAYLOAD_FIFO_CHILD") == "1" {
		f := newFixture(t)
		f.writeAggregate(priorContent)
		store := f.store()
		result := mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
		backups, err := store.SafetyBackups(f.rel)
		if err != nil || len(backups) != 1 {
			t.Fatalf("actual payload fixture: %v %v", backups, err)
		}
		if err := os.Rename(result.BackupPath, filepath.Join(t.TempDir(), "retained-payload")); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(result.BackupPath, 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(result.BackupPath)
		if err != nil {
			t.Fatal(err)
		}
		if data, err := backups[0].RawBytes(); len(data) != 0 || !errors.Is(err, fsboundary.ErrUnsafeFile) {
			t.Fatalf("FIFO accepted as raw payload: %q %v", data, err)
		}
		if after, err := os.Lstat(result.BackupPath); err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
			t.Fatalf("FIFO payload changed: %v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSafetyPayloadFIFORefusesWithoutBlocking$", "-test.count=1")
	command.Env = append(os.Environ(), "AM_SAFETY_PAYLOAD_FIFO_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bounded payload FIFO probe failed: %v %s", err, output)
	}
}
