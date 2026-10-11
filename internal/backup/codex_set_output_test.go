package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestCodexSetOutputPreservesChangedGenerations(t *testing.T) {
	for _, shape := range []string{"staged replacement", "final collision", "parent replacement"} {
		t.Run(shape, func(t *testing.T) {
			dir, foreign := t.TempDir(), t.TempDir()
			parent, err := fsboundary.OpenDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parent.Close() }()
			native, err := parent.NativeRoot()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = native.Close() }()
			output := codexSetOutput{parent: parent, native: native, path: dir, owned: make(map[string]fsboundary.Identity)}
			if err := output.write("archive.partial", 0o600, func(file *os.File) error { _, err := file.Write([]byte("inert archive generation")); return err }); err != nil {
				t.Fatal(err)
			}
			kept := filepath.Join(dir, "archive.partial")
			switch shape {
			case "staged replacement":
				if err := os.Rename(kept, filepath.Join(foreign, "retained-attempt")); err != nil {
					t.Fatal(err)
				}
			case "final collision":
				kept = filepath.Join(dir, "archive")
			case "parent replacement":
				if err := os.Rename(dir, filepath.Join(foreign, "retained-parent")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(kept, []byte("inert replacement bytes"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := output.publish("archive.partial", "archive"); !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("changed set member published: %v", err)
			}
			if err := output.cleanup(); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(kept); err != nil || string(data) != "inert replacement bytes" {
				t.Fatalf("cleanup modified replacement: %q %v", data, err)
			}
			if _, err := native.Lstat("archive"); shape != "final collision" && !os.IsNotExist(err) {
				t.Fatalf("unexpected final publication: %v", err)
			}
		})
	}
}
