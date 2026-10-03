package materialize

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: materialization/native-command-pruning-preserves-replacement
func TestNativeSkillMarkedFilePruning(t *testing.T) {
	for _, mutation := range []string{"replacement", "disappearance", "healthy", "dry-run"} {
		t.Run(mutation, func(t *testing.T) {
			root := t.TempDir()
			out := filepath.Join(root, "skills")
			if err := os.Mkdir(out, 0o755); err != nil {
				t.Fatal(err)
			}
			original := []byte("---\n_managed_by: agent-marketplace\n---\n\noriginal marked file\n")
			leaf := filepath.Join(out, "a-marked.md")
			for _, n := range []string{"a-marked.md", "b-marked.md"} {
				if err := os.WriteFile(filepath.Join(out, n), original, 0o640); err != nil {
					t.Fatal(err)
				}
			}
			unmarked := filepath.Join(out, "handwritten.md")
			if err := os.WriteFile(unmarked, []byte("handwritten file\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			unmarkedInfo, err := os.Lstat(unmarked)
			if err != nil {
				t.Fatal(err)
			}
			boundary := &destinationBoundary{repoRoot: root, roots: []string{out}}
			var hit bool
			var replacement os.FileInfo
			retained := filepath.Join(root, "retained.md")
			boundary.pruneClassificationHook = func(path string) {
				if path != leaf {
					return
				}
				hit = true
				switch mutation {
				case "replacement":
					if err := os.Rename(leaf, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(leaf, []byte("replacement user bytes\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					var err error
					replacement, err = os.Lstat(leaf)
					if err != nil {
						t.Fatal(err)
					}
				case "disappearance":
					if err := os.Remove(leaf); err != nil {
						t.Fatal(err)
					}
				}
			}
			report, err := syncSkillLinks(out, nil, mutation == "dry-run", true, portableSyncContext{boundary: boundary})
			if !hit {
				t.Fatal("regular-file classification seam not reached")
			}
			if mutation == "replacement" {
				if !errors.Is(err, fsboundary.ErrGenerationConflict) || slices.Contains(report.Removed, "a-marked.md") {
					t.Fatalf("replacement not refused: %+v %v", report, err)
				}
				assertNativePruneFileSnapshot(t, leaf, []byte("replacement user bytes\n"), replacement)
				if data, err := os.ReadFile(retained); err != nil || !bytes.Equal(data, original) {
					t.Fatalf("admitted bytes changed: %q %v", data, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"a-marked.md", "b-marked.md"}
				if mutation == "disappearance" {
					want = []string{"b-marked.md"}
				}
				if !slices.Equal(report.Removed, want) {
					t.Fatalf("removed=%v want=%v", report.Removed, want)
				}
				for _, n := range want {
					path := filepath.Join(out, n)
					if mutation == "dry-run" {
						if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) {
							t.Fatalf("dry-run changed marked file: %q %v", data, err)
						}
					} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("healthy marked file not removed: %s %v", n, err)
					}
				}
			}
			assertNativePruneFileSnapshot(t, unmarked, []byte("handwritten file\n"), unmarkedInfo)
		})
	}
}

func assertNativePruneFileSnapshot(t *testing.T, path string, want []byte, observed os.FileInfo) {
	t.Helper()
	data, err := os.ReadFile(path)
	info, statErr := os.Lstat(path)
	if err != nil || statErr != nil || !bytes.Equal(data, want) || !os.SameFile(info, observed) || info.Mode() != observed.Mode() || !info.ModTime().Equal(observed.ModTime()) {
		t.Fatalf("file generation changed: %s %q %v %v", path, data, err, statErr)
	}
}
