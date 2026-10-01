package ingest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// spec: marketplace-api/mirrored-hook-support-safe-publication
func TestReadHookSupportFileSafety(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "dangling", "hardlink", "fifo", "oversized", "directory-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "script")
			if err := os.WriteFile(outside, []byte("external bytes"), 0o755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "script")
			name := "script"
			var err error
			switch kind {
			case "regular":
				err = os.WriteFile(file, []byte("safe bytes"), 0o755)
			case "symlink":
				err = os.Symlink(outside, file)
			case "dangling":
				err = os.Symlink(filepath.Join(root, "absent"), file)
			case "hardlink":
				err = os.Link(outside, file)
			case "fifo":
				err = unix.Mkfifo(file, 0o600)
			case "oversized":
				err = os.WriteFile(file, make([]byte, 2*1024*1024+1), 0o644)
			case "directory-symlink":
				err = os.Symlink(filepath.Dir(outside), filepath.Join(root, "sub"))
				name = "sub/script"
			}
			if err != nil {
				t.Fatal(err)
			}
			data, executable, err := readHookSupportFile(root, name)
			if kind == "regular" {
				if err != nil || !executable || string(data) != "safe bytes" {
					t.Fatalf("valid read: %q %v %v", data, executable, err)
				}
			} else if !errors.Is(err, ErrHookSupportUnsafe) {
				t.Fatalf("unsafe read error identity: %v", err)
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != "external bytes" {
				t.Fatalf("external bytes changed: %q %v", data, err)
			}
		})
	}
}
