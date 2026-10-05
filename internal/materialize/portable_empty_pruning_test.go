package materialize

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestCleanEmptyPortableDirectoriesRelativeRoot(t *testing.T) {
	for _, alias := range []bool{false, true} {
		for _, captured := range []bool{false, true} {
			t.Run(map[bool]string{false: "direct", true: "scope alias"}[alias]+"/"+map[bool]string{false: "uncaptured", true: "captured"}[captured], func(t *testing.T) {
				base := t.TempDir()
				scope := filepath.Join(base, "canonical", "nested", "scope")
				if err := os.MkdirAll(scope, 0o755); err != nil {
					t.Fatal(err)
				}
				t.Chdir(base)
				lexicalScope := filepath.Join("canonical", "nested", "scope")
				if alias {
					lexicalScope = "scope-alias"
					if err := os.Symlink(scope, lexicalScope); err != nil {
						t.Fatal(err)
					}
				}
				root := filepath.Join(lexicalScope, ".factory")
				empty := filepath.Join(root, "droids", "nested")
				if err := os.MkdirAll(empty, 0o755); err != nil {
					t.Fatal(err)
				}
				var boundary *destinationBoundary
				if captured {
					boundary = &destinationBoundary{repoRoot: lexicalScope, roots: []string{root}}
				}
				cleanEmptyPortableArtifactDirs(root, "droids/nested", boundary)
				if _, err := os.Lstat(filepath.Join(root, "droids")); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("relative-root cleanup left empty tree: %v", err)
				}
				if info, err := os.Stat(root); err != nil || !info.IsDir() {
					t.Fatalf("cleanup removed authorized root: %v", err)
				}
				// A nonempty control must remain unchanged.
				if err := os.MkdirAll(empty, 0o755); err != nil {
					t.Fatal(err)
				}
				leaf := filepath.Join(empty, "handwritten.md")
				if err := os.WriteFile(leaf, []byte("handwritten control"), 0o640); err != nil {
					t.Fatal(err)
				}
				cleanEmptyPortableArtifactDirs(root, "droids/nested", boundary)
				if data, err := os.ReadFile(leaf); err != nil || string(data) != "handwritten control" {
					t.Fatalf("cleanup changed nonempty tree: %q %v", data, err)
				}
				// An intermediate alias, unlike the admitted scope alias, is unsafe.
				outside := t.TempDir()
				if err := os.Mkdir(filepath.Join(outside, "empty"), 0o755); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(root, "escape")
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}
				cleanEmptyPortableArtifactDirs(root, "escape/empty", boundary)
				if info, err := os.Stat(filepath.Join(outside, "empty")); err != nil || !info.IsDir() {
					t.Fatalf("cleanup traversed alias: %v", err)
				}
				if text, err := os.Readlink(link); err != nil || text != outside {
					t.Fatalf("cleanup changed alias: %q %v", text, err)
				}
			})
		}
	}
}

func TestRemoveEmptyPortableDirectoryRejectsEmptyPaths(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, paths := range [][2]string{{"", empty}, {root, ""}, {"", ""}} {
		if err := removeEmptyPortableDirectory(paths[0], paths[1], nil); !errors.Is(err, fsboundary.ErrUnsafePath) {
			t.Errorf("empty paths must refuse: %v", err)
		}
	}
	if info, err := os.Stat(empty); err != nil || !info.IsDir() {
		t.Fatalf("empty paths removed control: %v", err)
	}
}
