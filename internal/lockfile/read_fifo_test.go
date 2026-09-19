package lockfile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func TestReadBytesReadersRefuseFIFO(t *testing.T) {
	if os.Getenv("LOCK_FIFO_CHILD") == "1" {
		path := os.Getenv("LOCK_FIFO_PATH")
		mode := os.Getenv("LOCK_FIFO_MODE")
		var b []byte
		var info os.FileInfo
		var doc Document
		var err error
		switch mode {
		case "read":
			doc, err = Read(path)
		case "bytes":
			b, err = ReadBytes(path)
		case "info":
			b, info, err = ReadBytesInfo(path)
		default:
			t.Fatalf("unknown FIFO mode %q", mode)
		}
		if err == nil || !errors.Is(err, ErrUnsafeFile) || b != nil || info != nil || !reflect.DeepEqual(doc, Document{}) {
			t.Fatalf("mode=%s bytes=%q info=%v doc=%#v err=%v", mode, b, info, doc, err)
		}
		return
	}
	for _, mode := range []string{"read", "bytes", "info"} {
		for _, linked := range []bool{false, true} {
			t.Run(mode+"/linked="+map[bool]string{false: "false", true: "true"}[linked], func(t *testing.T) {
				dir, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				fifo := filepath.Join(dir, "fifo")
				if err := syscall.Mkfifo(fifo, 0o600); err != nil {
					t.Fatal(err)
				}
				path := fifo
				if linked {
					path = filepath.Join(dir, "link")
					if err := os.Symlink(fifo, path); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.Lstat(fifo)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				exe, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(ctx, exe, "-test.run", "^TestReadBytesReadersRefuseFIFO$", "-test.v")
				cmd.Env = append(os.Environ(), "LOCK_FIFO_CHILD=1", "LOCK_FIFO_PATH="+path, "LOCK_FIFO_MODE="+mode)
				out, err := cmd.CombinedOutput()
				if err != nil || ctx.Err() != nil {
					t.Fatalf("child error=%v context=%v output=%s", err, ctx.Err(), out)
				}
				after, err := os.Lstat(fifo)
				if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
					t.Fatalf("FIFO changed: %v", err)
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if linked {
					want = 2
				}
				if len(entries) != want {
					t.Fatalf("entries=%v want=%d", entries, want)
				}
			})
		}
	}
}
