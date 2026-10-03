package tui

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

func TestDetailSourcePreviewFIFORefusesWithoutBlocking(t *testing.T) {
	const marker, rootKey = "AM_PREVIEW_FIFO_CHILD", "AM_PREVIEW_FIFO_ROOT"
	if os.Getenv(marker) == "1" {
		root := os.Getenv(rootKey)
		content, truncated, err := readDetailSourceLimit(filepath.Join(root, "source.txt"), 8)
		if !errors.Is(err, fsboundary.ErrUnsafeFile) || content != "" || truncated {
			t.Fatalf("FIFO supplied source preview bytes: %q %v %v", content, truncated, err)
		}
		if content := renderBundleFile(root, "source.txt", 80); !strings.Contains(content, "Error reading file") {
			t.Fatalf("FIFO bundle preview did not refuse: %q", content)
		}
		return
	}
	root := t.TempDir()
	fifo := filepath.Join(root, "source.txt")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(fifo)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDetailSourcePreviewFIFORefusesWithoutBlocking$", "-test.count=1")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, marker+"=") && !strings.HasPrefix(env, rootKey+"=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, marker+"=1", rootKey+"="+root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("preview FIFO consumer did not refuse promptly: %v %v\n%s", err, ctx.Err(), output)
	}
	if after, err := os.Lstat(fifo); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("preview FIFO refusal changed leaf: %v", err)
	}
}
