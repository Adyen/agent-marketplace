package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/native-command-pruning-preserves-replacement
func TestNativeCommandSymlinkPruneClassification(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		for _, mutation := range []string{"replacement", "disappearance", "healthy"} {
			t.Run(fmt.Sprintf("dry-run=%v/%s", dryRun, mutation), func(t *testing.T) {
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				root := t.TempDir()
				dir := filepath.Join(root, "commands")
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				originalText := filepath.Join(cache.CacheDir(), "original-missing-target")
				for _, name := range []string{"a-stale.md", "b-stale.md"} {
					if err := os.Symlink(originalText, filepath.Join(dir, name)); err != nil {
						t.Fatal(err)
					}
				}
				userPath := filepath.Join(dir, "handwritten.md")
				userText := filepath.Join(root, "handwritten-missing-target")
				if err := os.Symlink(userText, userPath); err != nil {
					t.Fatal(err)
				}
				userInfo, err := os.Lstat(userPath)
				if err != nil {
					t.Fatal(err)
				}
				leaf := filepath.Join(dir, "a-stale.md")
				replacementText := filepath.Join(cache.CacheDir(), "different-managed-missing-target")
				var replacement os.FileInfo
				hit := false
				boundary := &destinationBoundary{repoRoot: root, roots: []string{dir}, pruneClassificationHook: func(path string) {
					if path != leaf {
						return
					}
					hit = true
					switch mutation {
					case "replacement":
						if err := os.Rename(leaf, filepath.Join(root, "retained-link")); err != nil {
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
					case "disappearance":
						if err := os.Remove(leaf); err != nil {
							t.Fatal(err)
						}
					}
				}}
				report, err := syncCommandFiles(dir, nil, dryRun, true, portableSyncContext{boundary: boundary})
				if !hit {
					t.Fatal("command classification hook was not reached")
				}
				if mutation == "replacement" {
					if !errors.Is(err, fsboundary.ErrGenerationConflict) || len(report.Removed) != 0 {
						t.Fatalf("classified managed replacement not refused: %+v %v", report, err)
					}
					assertCommandPruneLinkSnapshot(t, leaf, replacementText, replacement)
					if text, err := os.Readlink(filepath.Join(root, "retained-link")); err != nil || text != originalText {
						t.Fatalf("original link changed: %q %v", text, err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					want := []string{"a-stale.md", "b-stale.md"}
					if mutation == "disappearance" {
						want = []string{"b-stale.md"}
					}
					if !slices.Equal(report.Removed, want) {
						t.Fatalf("removed=%v want=%v", report.Removed, want)
					}
					for _, name := range want {
						_, err := os.Lstat(filepath.Join(dir, name))
						if dryRun && err != nil {
							t.Fatal(err)
						}
						if !dryRun && !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("healthy stale link retained: %s %v", name, err)
						}
					}
				}
				assertCommandPruneLinkSnapshot(t, userPath, userText, userInfo)
			})
		}
	}
}

// spec: materialization/native-command-pruning-preserves-replacement
func TestRunNativeCommandSymlinkPruneRefusesReplacement(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprint("dry-run=", dryRun), func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			original := resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "stale-command", Resource: ir.NewCommand("stale-command", "prune", "original inert body", nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}
			installed, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{original})
			if err != nil || diag.HasErrors(installed.Diags) {
				t.Fatalf("native install control: %+v %v", installed, err)
			}
			leaf := filepath.Join(repo, ".factory", "commands", "stale-command.md")
			if err := os.Remove(leaf); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(cache.CacheDir(), "legacy-missing-target"), leaf); err != nil {
				t.Fatal(err)
			}
			text := filepath.Join(cache.CacheDir(), "different-managed-native-target")
			var replacement os.FileInfo
			keep := resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "kept", Resource: ir.NewCommand("kept", "prune", "kept inert body", nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}
			report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{keep}, func(cfg *runConfig) {
				cfg.dryRun = dryRun
				cfg.pruneClassificationHook = func(path string) {
					if path != leaf {
						return
					}
					if err := os.Rename(leaf, filepath.Join(repo, "retained-native-link")); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(text, leaf); err != nil {
						t.Fatal(err)
					}
					var err error
					replacement, err = os.Lstat(leaf)
					if err != nil {
						t.Fatal(err)
					}
				}
			})
			if err != nil || replacement == nil || !hasDiagCode(report.Diags, "materialize.write.error") {
				t.Fatalf("native coded refusal missing: %+v %v", report, err)
			}
			for _, tr := range report.TargetReports() {
				if slices.Contains(tr.Removed, "commands/stale-command.md") || slices.Contains(tr.Removed, "stale-command.md") {
					t.Fatalf("refused link reported removed: %+v", tr)
				}
			}
			assertCommandPruneLinkSnapshot(t, leaf, text, replacement)
			if !dryRun {
				data, readErr := os.ReadFile(filepath.Join(repo, ".factory", "commands", "kept.md"))
				if readErr != nil || !bytes.Contains(data, []byte("kept inert body")) {
					t.Fatalf("selected native sibling was not installed: %q %v", data, readErr)
				}
			}
		})
	}
}

func assertCommandPruneLinkSnapshot(t *testing.T, path, text string, observed os.FileInfo) {
	t.Helper()
	got, err := os.Readlink(path)
	info, statErr := os.Lstat(path)
	if err != nil || statErr != nil || got != text || !os.SameFile(info, observed) || info.Mode() != observed.Mode() || !info.ModTime().Equal(observed.ModTime()) {
		t.Fatalf("link generation changed: %s %q %v %v", path, got, err, statErr)
	}
}
