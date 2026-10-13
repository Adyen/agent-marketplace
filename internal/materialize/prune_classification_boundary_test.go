package materialize

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: materialization/prune-directory-refusal-precedes-foreign-discovery
func TestPruneDirectoryClassificationRetainsCapturedRoot(t *testing.T) {
	for _, commandLane := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("commands=%v/dry-run=%v", commandLane, dryRun), func(t *testing.T) {
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				root, foreign := t.TempDir(), t.TempDir()
				dir := filepath.Join(root, "native")
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				name := "same-named-link"
				originalText := filepath.Join(root, "handwritten-target")
				foreignText := filepath.Join(cache.CacheDir(), "foreign-managed-target")
				original := filepath.Join(dir, name)
				foreignLeaf := filepath.Join(foreign, name)
				if err := os.Symlink(originalText, original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreignText, foreignLeaf); err != nil {
					t.Fatal(err)
				}
				originalInfo, err := os.Lstat(original)
				if err != nil {
					t.Fatal(err)
				}
				foreignInfo, err := os.Lstat(foreignLeaf)
				if err != nil {
					t.Fatal(err)
				}
				boundary := &destinationBoundary{repoRoot: root, roots: []string{dir}}
				call := func() (TargetReport, error) {
					if commandLane {
						var r TargetReport
						e := pruneStaleCommandFiles(dir, nil, dryRun, &r, portableSyncContext{boundary: boundary})
						return r, e
					}
					return syncSkillLinks(dir, nil, dryRun, true, portableSyncContext{boundary: boundary})
				}
				control, err := call()
				if err != nil || len(control.Removed) != 0 || !hasDiagCode(control.Diags, "materialize.collision.user-owned") {
					t.Fatalf("user-owned control: %+v %v", control, err)
				}
				retained := filepath.Join(root, "retained-native")
				hit := false
				boundary.pruneEnumerationHook = func(path string) {
					if path != dir {
						return
					}
					hit = true
					if err := os.Rename(dir, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(foreign, dir); err != nil {
						t.Fatal(err)
					}
				}
				report, err := call()
				if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) || len(report.Removed) != 0 {
					t.Fatalf("foreign ownership imported after enumeration: hit=%v report=%+v err=%v", hit, report, err)
				}
				assertPruneLinkSnapshot(t, filepath.Join(retained, name), originalText, originalInfo)
				assertPruneLinkSnapshot(t, foreignLeaf, foreignText, foreignInfo)
				boundary.pruneEnumerationHook = nil
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(retained, dir); err != nil {
					t.Fatal(err)
				}
				for _, n := range []string{"managed-link", "marked.md"} {
					path := filepath.Join(dir, n)
					if n == "managed-link" {
						if err := os.Symlink(filepath.Join(cache.CacheDir(), "healthy-missing-target"), path); err != nil {
							t.Fatal(err)
						}
					} else if err := os.WriteFile(path, []byte("---\n_managed_by: agent-marketplace\n---\n\ninert marked body\n"), 0o640); err != nil {
						t.Fatal(err)
					}
				}
				healthy, err := call()
				if err != nil || !slices.Equal(healthy.Removed, []string{"managed-link", "marked.md"}) {
					t.Fatalf("healthy classification/pruning: %+v %v", healthy, err)
				}
				assertPruneLinkSnapshot(t, original, originalText, originalInfo)
				for _, n := range healthy.Removed {
					_, err := os.Lstat(filepath.Join(dir, n))
					if dryRun && err != nil {
						t.Fatal(err)
					}
					if !dryRun && !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("healthy stale entry not removed: %s %v", n, err)
					}
				}
			})
		}
	}
}

func TestPruneDirectoryCaptureFailureClosesRoot(t *testing.T) {
	root, foreign := t.TempDir(), t.TempDir()
	dir, retained := filepath.Join(root, "native"), filepath.Join(root, "retained")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	boundary := &destinationBoundary{repoRoot: root, roots: []string{dir}, linkPublicationHook: func(path string) {
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, path); err != nil {
			t.Fatal(err)
		}
	}}
	countDescriptors := func() int {
		t.Helper()
		directory, err := os.Open("/dev/fd")
		if err != nil {
			t.Fatal(err)
		}
		names, readErr := directory.Readdirnames(-1)
		closeErr := directory.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("descriptor enumeration: %v %v", readErr, closeErr)
		}
		return len(names)
	}
	before := countDescriptors()
	for range 24 {
		captured, err := capturePruneDirectory(dir, boundary)
		if captured != nil || !errors.Is(err, fsboundary.ErrGenerationConflict) {
			t.Fatalf("failed capture retained a root: %v %v", captured, err)
		}
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(retained, dir); err != nil {
			t.Fatal(err)
		}
	}
	if after := countDescriptors(); after != before {
		t.Fatalf("failed captures leaked descriptors: before=%d after=%d", before, after)
	}
}

func assertPruneLinkSnapshot(t *testing.T, path, text string, observed os.FileInfo) {
	t.Helper()
	got, err := os.Readlink(path)
	info, statErr := os.Lstat(path)
	if err != nil || statErr != nil || got != text || !os.SameFile(info, observed) || info.Mode() != observed.Mode() || !info.ModTime().Equal(observed.ModTime()) {
		t.Fatalf("link generation changed: %s %q %v %v", path, got, err, statErr)
	}
}
