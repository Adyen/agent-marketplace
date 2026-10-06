package collection

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"golang.org/x/sys/unix"
)

func TestDiscoverCollectionSourceFIFORefusesWithoutBlocking(t *testing.T) {
	const marker, rootKey = "AM_COLLECTION_FIFO_CHILD", "AM_COLLECTION_FIFO_ROOT"
	if os.Getenv(marker) == "1" {
		got, err := DiscoverCollections(os.Getenv(rootKey), "", "", "", "")
		if !errors.Is(err, fsboundary.ErrUnsafeFile) || len(got) != 0 {
			t.Fatalf("FIFO collection did not refuse: %+v %v", got, err)
		}
		return
	}
	root := t.TempDir()
	dir := filepath.Join(root, "collections", "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, collectionFileName)
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(fifo)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDiscoverCollectionSourceFIFORefusesWithoutBlocking$", "-test.count=1")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, marker+"=") && !strings.HasPrefix(env, rootKey+"=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, marker+"=1", rootKey+"="+root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("collection FIFO consumer did not refuse promptly: %v %v\n%s", err, ctx.Err(), output)
	}
	if after, err := os.Lstat(fifo); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("collection FIFO refusal changed leaf: %v", err)
	}
}
