package materialize

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type treeUnrecognizedInfo struct{ os.FileInfo }

func (treeUnrecognizedInfo) Sys() any { return nil }

func TestPortableTreeInitialInspectionCleanup(t *testing.T) {
	for _, failure := range []string{"lstat", "identity", "replacement directory", "replacement alias"} {
		t.Run(failure, func(t *testing.T) {
			source, parent := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("source control"), 0o640); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(parent, "output")
			external := t.TempDir()
			if err := os.WriteFile(filepath.Join(external, "user.md"), []byte("external control"), 0o444); err != nil {
				t.Fatal(err)
			}
			outsideBefore := portableModeSnapshot(t, external)
			var stage string
			var replacementBefore map[string]os.FileInfo
			injected := &os.PathError{Op: "lstat", Path: "staged", Err: syscall.EIO}
			err := publishPortableTree(source, destination, nil, "", nil, func(name string) (os.FileInfo, error) {
				stage = filepath.Join(parent, name)
				info, err := os.Lstat(stage)
				if err != nil {
					t.Fatal(err)
				}
				switch failure {
				case "identity":
					return treeUnrecognizedInfo{info}, nil
				case "replacement directory", "replacement alias":
					if err := os.Rename(stage, filepath.Join(parent, "retained")); err != nil {
						t.Fatal(err)
					}
					if failure == "replacement alias" {
						if err := os.Symlink(external, stage); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.Mkdir(stage, 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(stage, "user.md"), []byte("replacement control"), 0o444); err != nil {
							t.Fatal(err)
						}
						replacementBefore = portableModeSnapshot(t, stage)
					}
				}
				return nil, injected
			})
			if stage == "" {
				t.Fatal("initial inspection fixture did not run")
			}
			if failure == "identity" {
				if !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Errorf("identity refusal lost stable cause: %v", err)
				}
			} else {
				var pathErr *os.PathError
				if !errors.Is(err, injected) || !errors.As(err, &pathErr) {
					t.Errorf("inspection failure lost cause: %v", err)
				}
			}
			if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("failed inspection published output: %v", err)
			}
			assertCleanupBytes(t, filepath.Join(source, "SKILL.md"), "source control")
			assertPortableModeSnapshot(t, external, outsideBefore)
			switch failure {
			case "lstat", "identity":
				assertNoPortableTreeStage(t, parent)
			case "replacement alias":
				if text, err := os.Readlink(stage); err != nil || text != external {
					t.Fatalf("changed replacement link: %q %v", text, err)
				}
			default:
				assertCleanupBytes(t, filepath.Join(stage, "user.md"), "replacement control")
				assertPortableModeSnapshot(t, stage, replacementBefore)
			}
		})
	}
}
