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

func TestSafetyEvidenceFIFORefusesWithoutBlocking(t *testing.T) {
	if os.Getenv("AM_SAFETY_EVIDENCE_FIFO_CHILD") == "1" {
		f := newFixture(t)
		f.writeAggregate(priorContent)
		store := f.store()
		result := mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
		manifest := metaPathFor(result.BackupPath)
		if err := os.Rename(manifest, filepath.Join(t.TempDir(), "retained-manifest")); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(manifest, 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if backups, err := store.SafetyBackups(f.rel); len(backups) != 0 || !errors.Is(err, fsboundary.ErrUnsafeFile) {
			t.Fatalf("FIFO supplied safety evidence: %v %v", backups, err)
		}
		if after, err := os.Lstat(manifest); err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
			t.Fatalf("FIFO evidence changed: %v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSafetyEvidenceFIFORefusesWithoutBlocking$", "-test.count=1")
	command.Env = append(os.Environ(), "AM_SAFETY_EVIDENCE_FIFO_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bounded safety evidence FIFO probe failed: %v %s", err, output)
	}
}
