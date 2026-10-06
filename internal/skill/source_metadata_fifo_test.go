package skill

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

func TestLegacySourceMetadataFIFORefusesWithoutBlocking(t *testing.T) {
	const marker, rootKey = "AM_LEGACY_METADATA_FIFO_CHILD", "AM_LEGACY_METADATA_FIFO_ROOT"
	if os.Getenv(marker) == "1" {
		root := os.Getenv(rootKey)
		got, malformed, err := DiscoverSkills(root)
		if err != nil || len(got) != 0 || len(malformed) != 1 || !errors.Is(malformed[0].Error, fsboundary.ErrUnsafeFile) {
			t.Fatalf("legacy FIFO did not retain refusal: %+v %+v %v", got, malformed, err)
		}
		if name := DeclaredNameFromFile(filepath.Join(root, "probe")); name != "" {
			t.Fatalf("FIFO supplied authored name: %q", name)
		}
		return
	}
	root := t.TempDir()
	dir := filepath.Join(root, "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "SKILL.md")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(fifo)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLegacySourceMetadataFIFORefusesWithoutBlocking$", "-test.count=1")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, marker+"=") && !strings.HasPrefix(env, rootKey+"=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, marker+"=1", rootKey+"="+root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("legacy FIFO consumer did not refuse promptly: %v %v\n%s", err, ctx.Err(), output)
	}
	if after, err := os.Lstat(fifo); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("legacy FIFO refusal changed leaf: %v", err)
	}
}
