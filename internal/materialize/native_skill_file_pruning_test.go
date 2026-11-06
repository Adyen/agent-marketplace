package materialize

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: materialization/native-command-pruning-preserves-replacement
func TestNativeSkillMarkedFilePruning(t *testing.T) { testNativeMarkedFilePruning(t, false) }

// spec: materialization/native-command-pruning-preserves-replacement
func TestNativeCommandFilePruneMissingParity(t *testing.T) { testNativeMarkedFilePruning(t, true) }

func testNativeMarkedFilePruning(t *testing.T, commandLane bool) {
	for _, testCase := range []string{"replacement", "disappearance", "healthy", "dry-healthy", "dry-replacement", "dry-disappearance", "unsafe-parent", "dry-unsafe-parent"} {
		t.Run(testCase, func(t *testing.T) {
			mutation := strings.TrimPrefix(testCase, "dry-")
			dryRun := strings.HasPrefix(testCase, "dry-")
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
			originalInfo, err := os.Lstat(leaf)
			if err != nil {
				t.Fatal(err)
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
				case "unsafe-parent":
					if err := os.Rename(out, filepath.Join(root, "retained-skills")); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(t.TempDir(), out); err != nil {
						t.Fatal(err)
					}
				case "disappearance":
					if err := os.Remove(leaf); err != nil {
						t.Fatal(err)
					}
				}
			}
			var report TargetReport
			if commandLane {
				report, err = syncCommandFiles(out, nil, dryRun, true, portableSyncContext{boundary: boundary})
			} else {
				report, err = syncSkillLinks(out, nil, dryRun, true, portableSyncContext{boundary: boundary})
			}
			if !hit {
				t.Fatal("regular-file classification seam not reached")
			}
			if mutation == "unsafe-parent" {
				if !errors.Is(err, fsboundary.ErrUnsafePath) || len(report.Removed) != 0 {
					t.Fatalf("unsafe parent treated as missing: %+v %v", report, err)
				}
				assertNativePruneFileSnapshot(t, filepath.Join(root, "retained-skills", "a-marked.md"), original, originalInfo)
				assertNativePruneFileSnapshot(t, filepath.Join(root, "retained-skills", "handwritten.md"), []byte("handwritten file\n"), unmarkedInfo)
				return
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
					if dryRun {
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

func TestNativeSkillMarkedFilePruneKeepsEnumeratedGeneration(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "skills")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(out, "a-marked.md")
	original := []byte("---\n_managed_by: agent-marketplace\n---\n\noriginal marked file\n")
	if err := os.WriteFile(leaf, original, 0o640); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(root, "retained.md")
	replacementBytes := []byte("---\n_managed_by: agent-marketplace\n---\n\nreplacement marked file\n")
	var replacement os.FileInfo
	skillPruneObservedHook = func(path string) {
		if path != leaf || replacement != nil {
			return
		}
		if err := os.Rename(leaf, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(leaf, replacementBytes, 0o640); err != nil {
			t.Fatal(err)
		}
		var err error
		if replacement, err = os.Lstat(leaf); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { skillPruneObservedHook = nil })
	boundary := &destinationBoundary{repoRoot: root, roots: []string{out}}
	report, err := syncSkillLinks(out, nil, false, true, portableSyncContext{boundary: boundary})
	if replacement == nil {
		t.Fatal("enumeration seam not reached")
	}
	if !errors.Is(err, fsboundary.ErrGenerationConflict) || slices.Contains(report.Removed, "a-marked.md") {
		t.Fatalf("marked replacement after enumeration was pruned: %+v %v", report, err)
	}
	assertNativePruneFileSnapshot(t, leaf, replacementBytes, replacement)
	if data, err := os.ReadFile(retained); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("enumerated generation changed: %q %v", data, err)
	}
}
