package materialize

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: materialization/native-managed-link-pruning-refuses-replacement-generation
func TestNativePruneClassificationSnapshot(t *testing.T) {
	for _, artifactLane := range []bool{false, true} {
		name := "skills"
		if artifactLane {
			name = "artifacts"
		}
		t.Run(name, func(t *testing.T) {
			for _, testCase := range []string{"managed-replacement", "disappearance", "healthy", "dry-healthy", "dry-managed-replacement", "dry-disappearance", "unsafe-parent", "dry-unsafe-parent"} {
				t.Run(testCase, func(t *testing.T) {
					mutation := strings.TrimPrefix(testCase, "dry-")
					dryRun := strings.HasPrefix(testCase, "dry-")
					t.Setenv("XDG_CACHE_HOME", t.TempDir())
					root := t.TempDir()
					out := filepath.Join(root, "native")
					if err := os.Mkdir(out, 0o755); err != nil {
						t.Fatal(err)
					}
					leaf := filepath.Join(out, "a-stale")
					for _, n := range []string{"a-stale", "b-stale"} {
						if err := os.Symlink(filepath.Join(cache.CacheDir(), "original"), filepath.Join(out, n)); err != nil {
							t.Fatal(err)
						}
					}
					boundary := &destinationBoundary{repoRoot: root, roots: []string{out}}
					var replacement os.FileInfo
					var hit bool
					replacementText := filepath.Join(cache.CacheDir(), "different-managed-generation")
					boundary.pruneClassificationHook = func(path string) {
						if path != leaf {
							return
						}
						hit = true
						switch mutation {
						case "managed-replacement":
							if err := os.Rename(leaf, filepath.Join(root, "original-link")); err != nil {
								t.Fatal(err)
							}
							if err := os.Symlink(replacementText, leaf); err != nil {
								t.Fatal(err)
							}
							var err error
							replacement, err = os.Lstat(leaf)
							if err != nil {
								t.Fatal(err)
							}
						case "unsafe-parent":
							if err := os.Rename(out, filepath.Join(root, "retained-directory")); err != nil {
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
					var removed []string
					var err error
					if artifactLane {
						r, _, e := syncArtifactLinks(out, nil, cache.CacheDir(), dryRun, true, nil, portableSyncContext{boundary: boundary})
						removed, err = r.Removed, e
					} else {
						r, e := syncSkillLinks(out, nil, dryRun, true, portableSyncContext{boundary: boundary})
						removed, err = r.Removed, e
					}
					if mutation == "unsafe-parent" {
						if !hit || !errors.Is(err, fsboundary.ErrUnsafePath) || len(removed) != 0 {
							t.Fatalf("unsafe missing parent swallowed: hit=%v removed=%v err=%v", hit, removed, err)
						}
						for _, n := range []string{"a-stale", "b-stale"} {
							if _, e := os.Readlink(filepath.Join(root, "retained-directory", n)); e != nil {
								t.Fatal(e)
							}
						}
						return
					}
					if mutation == "managed-replacement" {
						if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) {
							t.Fatalf("classified replacement not refused: hit=%v err=%v", hit, err)
						}
						if slices.Contains(removed, "a-stale") {
							t.Fatalf("refused entry reported removed: %v", removed)
						}
						text, e := os.Readlink(leaf)
						info, statErr := os.Lstat(leaf)
						if e != nil || statErr != nil || text != replacementText || !os.SameFile(info, replacement) || info.Mode() != replacement.Mode() || !info.ModTime().Equal(replacement.ModTime()) {
							t.Fatalf("replacement generation changed: %q %v %v", text, e, statErr)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					want := []string{"a-stale", "b-stale"}
					if mutation == "disappearance" {
						want = []string{"b-stale"}
						if !hit {
							t.Fatal("disappearance seam not reached")
						}
					}
					if !slices.Equal(removed, want) {
						t.Fatalf("removed=%v want=%v", removed, want)
					}
					for _, n := range []string{"a-stale", "b-stale"} {
						_, e := os.Lstat(filepath.Join(out, n))
						if dryRun && (mutation != "disappearance" || n != "a-stale") && e != nil {
							t.Fatal(e)
						}
						if !dryRun && !errors.Is(e, os.ErrNotExist) {
							t.Fatalf("stale sibling remained: %s %v", n, e)
						}
					}
				})
			}
		})
	}
}
