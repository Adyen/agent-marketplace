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

func TestPortableFileStageCleanup(t *testing.T) {
	for _, scenario := range []string{"owned", "published", "inspect failure", "unlink failure", "replacement", "external alias"} {
		t.Run(scenario, func(t *testing.T) {
			path := t.TempDir()
			parent, err := fsboundary.OpenDir(path)
			if err != nil {
				t.Fatal(err)
			}
			parentClosed := false
			t.Cleanup(func() {
				if !parentClosed {
					if err := parent.Close(); err != nil {
						t.Error(err)
					}
				}
			})
			native, err := parent.NativeRoot()
			if err != nil {
				t.Fatal(err)
			}
			nativeClosed := false
			t.Cleanup(func() {
				if !nativeClosed {
					if err := native.Close(); err != nil {
						t.Error(err)
					}
				}
			})
			staged, name, err := parent.CreateTemp(".agent-marketplace-portable-*")
			if err != nil {
				t.Fatal(err)
			}
			created, err := staged.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := staged.Write([]byte("attempt-owned")); err != nil {
				t.Fatal(err)
			}
			if err := staged.Close(); err != nil {
				t.Fatal(err)
			}
			control := filepath.Join(path, "installed.md")
			if err := os.WriteFile(control, []byte("installed control"), 0o640); err != nil {
				t.Fatal(err)
			}
			stage := filepath.Join(path, name)
			var wantErr error
			switch scenario {
			case "published":
				if err := native.Rename(name, "published.md"); err != nil {
					t.Fatal(err)
				}
			case "inspect failure":
				if err := native.Close(); err != nil {
					t.Fatal(err)
				}
				nativeClosed = true
				wantErr = fs.ErrClosed
			case "unlink failure":
				if err := parent.Close(); err != nil {
					t.Fatal(err)
				}
				parentClosed = true
				wantErr = syscall.EBADF
			case "replacement", "external alias":
				if err := native.Rename(name, "retained.md"); err != nil {
					t.Fatal(err)
				}
				if scenario == "replacement" {
					if err := os.WriteFile(stage, []byte("handwritten replacement"), 0o640); err != nil {
						t.Fatal(err)
					}
				} else {
					external := filepath.Join(t.TempDir(), "foreign.md")
					if err := os.WriteFile(external, []byte("handwritten replacement"), 0o640); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(external, stage); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = cleanupPortableFileStage(parent, native, name, created)
			if wantErr != nil {
				if !errors.Is(err, wantErr) {
					t.Errorf("cleanup discarded %v: %v", wantErr, err)
				}
				if scenario == "inspect failure" {
					var pathErr *os.PathError
					if !errors.As(err, &pathErr) {
						t.Errorf("cleanup lost PathError: %v", err)
					}
				}
			} else if err != nil {
				t.Errorf("cleanup failed: %v", err)
			}
			assertFileCleanupBytes(t, control, "installed control")
			switch scenario {
			case "owned", "published":
				if _, err := os.Lstat(stage); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("owned staging residue: %v", err)
				}
				if scenario == "published" {
					assertFileCleanupBytes(t, filepath.Join(path, "published.md"), "attempt-owned")
				}
			case "inspect failure", "unlink failure":
				assertFileCleanupBytes(t, stage, "attempt-owned")
				// Retry through fresh captures, clearing the real descriptor failure.
				fresh, err := fsboundary.OpenDir(path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := fresh.Close(); err != nil {
						t.Error(err)
					}
				}()
				freshNative, err := fresh.NativeRoot()
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := freshNative.Close(); err != nil {
						t.Error(err)
					}
				}()
				if err := cleanupPortableFileStage(fresh, freshNative, name, created); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(stage); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("retry left staging residue: %v", err)
				}
			case "replacement", "external alias":
				assertFileCleanupBytes(t, stage, "handwritten replacement")
				assertFileCleanupBytes(t, filepath.Join(path, "retained.md"), "attempt-owned")
				if err := native.Remove("retained.md"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func assertFileCleanupBytes(t *testing.T, path, want string) {
	t.Helper()
	if data, err := os.ReadFile(path); err != nil || string(data) != want {
		t.Fatalf("bytes at %s = %q, %v; want %q", path, data, err, want)
	}
}
