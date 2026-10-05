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

// Only the failing OS operation is injected; all identity and preservation
// checks use real objects under the same captured root as publication cleanup.
type treeCleanupRoot struct {
	*os.Root
	inspectErr, removeErr error
	removeCalled          bool
}

func (r *treeCleanupRoot) Lstat(name string) (os.FileInfo, error) {
	if r.inspectErr != nil {
		return nil, r.inspectErr
	}
	return r.Root.Lstat(name)
}

func (r *treeCleanupRoot) RemoveAll(name string) error {
	r.removeCalled = true
	if r.removeErr != nil {
		return r.removeErr
	}
	return r.Root.RemoveAll(name)
}

func TestPortableTreeStageCleanup(t *testing.T) {
	for _, scenario := range []string{"owned", "published", "inspect failure", "remove failure", "replacement", "external alias"} {
		t.Run(scenario, func(t *testing.T) {
			parent := t.TempDir()
			stage := filepath.Join(parent, ".agent-marketplace-portable-tree-probe")
			if err := os.Mkdir(stage, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stage, "SKILL.md"), []byte("attempt-owned"), 0o640); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(stage)
			if err != nil {
				t.Fatal(err)
			}
			createdID, err := fsboundary.FileInfoIdentity(info)
			if err != nil {
				t.Fatal(err)
			}
			native, err := os.OpenRoot(parent)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := native.Close(); err != nil {
					t.Error(err)
				}
			})
			root := &treeCleanupRoot{Root: native}
			var wantErr error
			wantRemove := scenario == "owned" || scenario == "remove failure"
			switch scenario {
			case "published":
				if err := native.Rename(filepath.Base(stage), "published"); err != nil {
					t.Fatal(err)
				}
			case "inspect failure":
				wantErr = &os.PathError{Op: "lstat", Path: stage, Err: syscall.EIO}
				root.inspectErr = wantErr
			case "remove failure":
				wantErr = &os.PathError{Op: "remove", Path: stage, Err: syscall.EACCES}
				root.removeErr = wantErr
			case "replacement", "external alias":
				if err := native.Rename(filepath.Base(stage), "retained"); err != nil {
					t.Fatal(err)
				}
				if scenario == "replacement" {
					if err := os.Mkdir(stage, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(stage, "SKILL.md"), []byte("handwritten replacement"), 0o640); err != nil {
						t.Fatal(err)
					}
				} else {
					external := t.TempDir()
					if err := os.WriteFile(filepath.Join(external, "SKILL.md"), []byte("handwritten replacement"), 0o640); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(external, stage); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = cleanupPortableTreeStage(root, filepath.Base(stage), createdID)
			if wantErr != nil {
				var pathErr *os.PathError
				if !errors.Is(err, wantErr) || !errors.As(err, &pathErr) {
					t.Errorf("cleanup lost typed failure: %v", err)
				}
			} else if err != nil {
				t.Errorf("cleanup failed: %v", err)
			}
			if root.removeCalled != wantRemove {
				t.Errorf("remove called = %v, want %v", root.removeCalled, wantRemove)
			}
			switch scenario {
			case "owned", "published":
				if _, err := os.Lstat(stage); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("stage still exists: %v", err)
				}
				if scenario == "published" {
					assertCleanupBytes(t, filepath.Join(parent, "published", "SKILL.md"), "attempt-owned")
				}
			case "inspect failure", "remove failure":
				assertCleanupBytes(t, filepath.Join(stage, "SKILL.md"), "attempt-owned")
				// Clear the injected failure and retry so the test leaves no owned residue.
				root.inspectErr, root.removeErr = nil, nil
				if err := cleanupPortableTreeStage(root, filepath.Base(stage), createdID); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(stage); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("retry left stage: %v", err)
				}
			case "replacement", "external alias":
				assertCleanupBytes(t, filepath.Join(stage, "SKILL.md"), "handwritten replacement")
				assertCleanupBytes(t, filepath.Join(parent, "retained", "SKILL.md"), "attempt-owned")
				if err := native.RemoveAll("retained"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPortableTreePublicationPreservesReplacedStage(t *testing.T) {
	parent, source := t.TempDir(), t.TempDir()
	destination := filepath.Join(parent, "installed")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "SKILL.md"), []byte("installed control"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("valid new content"), 0o640); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	var stage string
	boundary := &destinationBoundary{repoRoot: parent, linkPublicationHook: func(string) {
		stages, err := filepath.Glob(filepath.Join(parent, ".agent-marketplace-portable-tree-*"))
		if err != nil || len(stages) != 1 {
			t.Fatalf("stage fixture: %v %v", stages, err)
		}
		stage = stages[0]
		if err := os.Rename(stage, filepath.Join(parent, "retained")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(stage, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "SKILL.md"), []byte("handwritten stage replacement"), 0o640); err != nil {
			t.Fatal(err)
		}
	}}
	err = publishPortableTree(source, destination, observed, "", boundary)
	if stage == "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("stage replacement not refused: %v", err)
	}
	assertCleanupBytes(t, filepath.Join(destination, "SKILL.md"), "installed control")
	assertCleanupBytes(t, filepath.Join(stage, "SKILL.md"), "handwritten stage replacement")
	assertCleanupBytes(t, filepath.Join(parent, "retained", "SKILL.md"), "valid new content")
	if err := os.RemoveAll(filepath.Join(parent, "retained")); err != nil {
		t.Fatal(err)
	}
}

func assertCleanupBytes(t *testing.T, path, want string) {
	t.Helper()
	if data, err := os.ReadFile(path); err != nil || string(data) != want {
		t.Fatalf("bytes at %s = %q, %v; want %q", path, data, err, want)
	}
}
