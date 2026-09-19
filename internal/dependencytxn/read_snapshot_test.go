package dependencytxn

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadSnapshotReturnsBytesGenerationAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte("apiVersion: agent-marketplace/v2\nregistries: []\n")
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatal(err)
	}
	raw, generation, mode, err := ReadSnapshot(path)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if string(raw) != string(content) {
		t.Fatalf("bytes = %q", raw)
	}
	if !generation.Exists || generation.Digest != Digest(content) {
		t.Fatalf("generation = %#v", generation)
	}
	if mode != 0o640 {
		t.Fatalf("mode = %#o", mode)
	}
}

func TestReadSnapshotMissingReturnsZero(t *testing.T) {
	raw, generation, mode, err := ReadSnapshot(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("ReadSnapshot missing: %v", err)
	}
	if raw != nil || generation.Exists || generation.Digest != "" || mode != 0 {
		t.Fatalf("expected zero snapshot, got bytes=%q generation=%#v mode=%#o", raw, generation, mode)
	}
}

func TestReadSnapshotRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	raw, generation, _, err := ReadSnapshot(link)
	if err == nil {
		t.Fatalf("expected symlink refusal, got bytes=%q generation=%#v", raw, generation)
	}
	if !errors.Is(err, ErrUnsafePath) && !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("expected unsafe-path refusal, got %v", err)
	}
}

func TestReadSnapshotRefusesFIFO(t *testing.T) {
	if os.Getenv("DEPENDENCY_FIFO_CHILD") == "1" {
		path := os.Getenv("DEPENDENCY_FIFO_PATH")
		reader := os.Getenv("DEPENDENCY_FIFO_MODE")
		var raw []byte
		var generation Generation
		var mode os.FileMode
		var err error
		switch reader {
		case "snapshot":
			raw, generation, mode, err = ReadSnapshot(path)
		case "generation":
			generation, err = ReadGeneration(path)
		case "mode":
			generation, mode, err = ReadGenerationAndMode(path)
		case "recover":
			err = Recover(context.Background(), filepath.Dir(path))
		default:
			t.Fatalf("unknown FIFO reader %q", reader)
		}
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("reader %s: expected unsafe-path refusal, got %v", reader, err)
		}
		if reader == "recover" && !errors.Is(err, ErrRecoveryBlocked) {
			t.Fatalf("journal reader not reached: %v", err)
		}
		if raw != nil || generation != (Generation{}) || mode != 0 {
			t.Fatalf("refusal returned data: %q %#v %o", raw, generation, mode)
		}
		return
	}
	for _, mode := range []string{"snapshot", "generation", "mode", "recover"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "fifo")
			if mode == "recover" {
				path = filepath.Join(dir, ".dependencytxn-fifo.json")
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, exe, "-test.run", "^TestReadSnapshotRefusesFIFO$", "-test.v")
			cmd.Env = append(os.Environ(), "DEPENDENCY_FIFO_CHILD=1", "DEPENDENCY_FIFO_PATH="+path, "DEPENDENCY_FIFO_MODE="+mode)
			if out, err := cmd.CombinedOutput(); err != nil || ctx.Err() != nil {
				t.Fatalf("child error=%v context=%v\n%s", err, ctx.Err(), out)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
				t.Fatalf("FIFO identity/type changed: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
				t.Fatalf("FIFO read left residue: %v error=%v", entries, err)
			}
		})
	}
}
