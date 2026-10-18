package materialize

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func TestArtifactLinkCheckComparesCacheContent(t *testing.T) {
	for _, change := range []string{"unchanged", "content", "missing", "user-owned"} {
		t.Run(change, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			out, cacheDir := t.TempDir(), filepath.Join(cache.CacheDir(), "compiled")
			artifacts := []artifact.Artifact{{Path: "manifest.json", Bytes: []byte(`{"value":"original"}`), Mode: 0o644}}
			if _, err := materializeArtifactLinkCache(cacheDir, artifacts, false, false); err != nil {
				t.Fatal(err)
			}
			if report, _, err := syncArtifactLinks(out, artifacts, cacheDir, false, false, nil); err != nil || len(report.Written) != 1 {
				t.Fatalf("install control: %+v %v", report, err)
			}
			output := filepath.Join(out, "manifest.json")
			linkInfo, err := os.Lstat(output)
			if err != nil {
				t.Fatal(err)
			}
			cached := filepath.Join(cacheDir, "manifest.json")
			before, mode, mtime := fileSnapshot(t, cached)
			switch change {
			case "content":
				artifacts[0].Bytes = []byte(`{"value":"updated"}`)
			case "missing":
				if err := os.Remove(cached); err != nil {
					t.Fatal(err)
				}
			case "user-owned":
				if err := os.Remove(output); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(output, []byte("handwritten"), 0o644); err != nil {
					t.Fatal(err)
				}
				artifacts[0].Bytes = []byte(`{"value":"updated"}`)
			}
			report, diagnostics, err := syncArtifactLinks(out, artifacts, cacheDir, true, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantWritten := 0
			if change == "content" || change == "missing" {
				wantWritten = 1
			}
			if len(report.Written) != wantWritten || len(report.Skipped) != 1-wantWritten {
				t.Fatalf("cache drift classification: %+v", report)
			}
			if change == "user-owned" {
				if !hasDiagCode(diagnostics, "materialize.collision.user-owned") {
					t.Fatalf("user collision lost its diagnostic: %+v", diagnostics)
				}
				if data, err := os.ReadFile(output); err != nil || string(data) != "handwritten" {
					t.Fatalf("user output changed: %q %v", data, err)
				}
			} else {
				assertCommandPruneLinkSnapshot(t, output, cached, linkInfo)
			}
			if change == "missing" {
				if _, err := os.Lstat(cached); !os.IsNotExist(err) {
					t.Fatalf("check repaired missing content: %v", err)
				}
			} else {
				assertFileUnchanged(t, cached, before, mode, mtime)
			}
		})
	}
}

func TestLinkedCacheCheckPreservesCompiledModeNoOp(t *testing.T) {
	root := t.TempDir()
	compiled := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(compiled, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	before, mode, mtime := fileSnapshot(t, compiled)
	link := skillLink{target: root, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("body"), Mode: 0o644}}}
	if changed, err := linkedCacheChanged(link); err != nil || changed {
		t.Fatalf("compiled permissions real sync preserves must not cause perpetual drift: %v %v", changed, err)
	}
	assertFileUnchanged(t, compiled, before, mode, mtime)
	link.artifacts[0].Bytes = []byte("updated body")
	if changed, err := linkedCacheChanged(link); err != nil || !changed {
		t.Fatalf("content control did not detect drift: %v %v", changed, err)
	}
}

func TestLinkedCacheCheckErrorsRetainOperationPathAndCause(t *testing.T) {
	for _, failure := range []string{"support-walk", "cache-compare", "support-stat"} {
		t.Run(failure, func(t *testing.T) {
			source, cacheDir := t.TempDir(), filepath.Join(t.TempDir(), "cache")
			if err := os.WriteFile(filepath.Join(source, "reference.txt"), []byte("support"), 0o644); err != nil {
				t.Fatal(err)
			}
			link := skillLink{target: cacheDir, sourceDir: source, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("body"), Mode: 0o644}}}
			if err := prepareSkillLinkCache(link); err != nil {
				t.Fatal(err)
			}
			compiled := filepath.Join(cacheDir, "SKILL.md")
			before, mode, mtime := fileSnapshot(t, compiled)
			support := filepath.Join(cacheDir, "reference.txt")
			supportBefore, supportMode, supportMtime := fileSnapshot(t, support)
			wantOperation, wantPath := "", cacheDir
			var wantCause error
			mutated := false
			switch failure {
			case "support-walk":
				link.sourceDir = filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(link.sourceDir, []byte("not support"), 0o644); err != nil {
					t.Fatal(err)
				}
				wantOperation, wantCause = "read desired support", fsboundary.ErrUnsafePath
			case "cache-compare":
				if err := os.Link(compiled, filepath.Join(t.TempDir(), "cache-alias")); err != nil {
					t.Fatal(err)
				}
				wantOperation, wantCause = "compare linked cache", fsboundary.ErrUnsafeFile
			case "support-stat":
				wantOperation, wantPath, wantCause = "inspect linked cache support", support, os.ErrNotExist
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(sdkStaleReadObserver{
					Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}),
					mutate: func(phase, path string) {
						if mutated || phase != "sdk-stale-directory" || path != cacheDir {
							return
						}
						mutated = true
						if err := os.Remove(support); err != nil {
							t.Fatal(err)
						}
					},
				}))
			}
			changed, err := linkedCacheChanged(link)
			if changed || !errors.Is(err, wantCause) {
				t.Fatalf("comparison did not retain refusal identity: changed=%t err=%v, want %v", changed, err, wantCause)
			}
			if !strings.Contains(err.Error(), wantOperation) || !strings.Contains(err.Error(), wantPath) {
				t.Fatalf("comparison error lacks operation %q or path %q: %v", wantOperation, wantPath, err)
			}
			assertFileUnchanged(t, compiled, before, mode, mtime)
			if failure == "support-stat" {
				if !mutated {
					t.Fatal("support-stat failure point was not exercised")
				}
				if _, err := os.Lstat(support); !os.IsNotExist(err) {
					t.Fatalf("check repaired removed cache support: %v", err)
				}
			} else {
				assertFileUnchanged(t, support, supportBefore, supportMode, supportMtime)
			}
			if stages, err := filepath.Glob(filepath.Join(filepath.Dir(cacheDir), ".support-staging-*")); err != nil || len(stages) != 0 {
				t.Fatalf("read-only failure left staging: %v %v", stages, err)
			}
		})
	}
}
