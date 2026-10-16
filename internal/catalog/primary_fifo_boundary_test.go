package catalog

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLoadPrimarySourceFIFORefusesWithoutBlocking(t *testing.T) {
	const marker = "AM_PRIMARY_SOURCE_FIFO_CHILD"
	if os.Getenv(marker) == "1" {
		root := os.Getenv("AM_PRIMARY_SOURCE_FIFO_ROOT")
		cat, err := Load(root, "claude")
		if err != nil {
			t.Fatal(err)
		}
		good := false
		for _, entry := range cat.Entries {
			good = good || entry.Name == "good"
			if entry.Name == "fifo" {
				t.Fatal("FIFO primary imported")
			}
		}
		if !good || len(cat.Diagnostics) == 0 {
			t.Fatalf("FIFO refusal lost sibling/diagnostic: %+v", cat)
		}
		return
	}
	root := t.TempDir()
	for _, name := range []string{"good", "fifo"} {
		if err := os.MkdirAll(filepath.Join(root, "skills", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "good", "SKILL.md"), []byte("---\nname: good\ndescription: good\n---\n\ninert body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "skills", "fifo", "SKILL.md")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(fifo)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoadPrimarySourceFIFORefusesWithoutBlocking$", "-test.count=1")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, marker+"=") && !strings.HasPrefix(env, "AM_PRIMARY_SOURCE_FIFO_ROOT=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, marker+"=1", "AM_PRIMARY_SOURCE_FIFO_ROOT="+root)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("primary FIFO consumer did not refuse promptly: %v %v\n%s", err, ctx.Err(), output)
	}
	if after, err := os.Lstat(fifo); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("primary FIFO refusal changed leaf: %v", err)
	}
}
