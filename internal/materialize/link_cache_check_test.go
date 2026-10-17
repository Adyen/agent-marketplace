package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
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
