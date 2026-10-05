package materialize

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/prune-directory-refusal-precedes-foreign-discovery
func TestPruneDirectoryMarkerReadRefusals(t *testing.T) {
	for _, commandLane := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			for _, mutation := range []string{"hardlink", "hardlink-after-enumeration", "replacement", "escaping-alias", "disappearance", "healthy"} {
				t.Run(fmt.Sprintf("commands=%v/dry-run=%v/%s", commandLane, dryRun, mutation), func(t *testing.T) {
					root, external := t.TempDir(), t.TempDir()
					dir := filepath.Join(root, "native")
					if err := os.Mkdir(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					marked := []byte("---\n_managed_by: agent-marketplace\n---\n\ninert marked payload\n")
					externalPath := filepath.Join(external, "external.md")
					if err := os.WriteFile(externalPath, marked, 0o640); err != nil {
						t.Fatal(err)
					}
					leaf := filepath.Join(dir, "a-probe.md")
					if mutation == "hardlink" {
						if err := os.Link(externalPath, leaf); err != nil {
							t.Fatal(err)
						}
						if isManagedCommandFile(leaf) {
							t.Fatal("marker bool helper accepted a hardlink")
						}
					} else if err := os.WriteFile(leaf, marked, 0o640); err != nil {
						t.Fatal(err)
					}
					externalInfo, err := os.Lstat(externalPath)
					if err != nil {
						t.Fatal(err)
					}
					probeInfo, err := os.Lstat(leaf)
					if err != nil {
						t.Fatal(err)
					}
					managedPath, userPath := filepath.Join(dir, "b-managed.md"), filepath.Join(dir, "c-user.md")
					if err := os.WriteFile(managedPath, marked, 0o640); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(userPath, []byte("ordinary handwritten file\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					managedInfo, err := os.Lstat(managedPath)
					if err != nil {
						t.Fatal(err)
					}
					userInfo, err := os.Lstat(userPath)
					if err != nil {
						t.Fatal(err)
					}
					boundary := &destinationBoundary{repoRoot: root, roots: []string{dir}}
					retained := filepath.Join(root, "retained-probe.md")
					var replacement os.FileInfo
					var retainedInfo os.FileInfo
					hit := false
					boundary.pruneMarkerReadHook = func(path string) {
						if path != leaf {
							return
						}
						hit = true
						switch mutation {
						case "hardlink-after-enumeration":
							if err := os.Remove(leaf); err != nil {
								t.Fatal(err)
							}
							if err := os.Link(externalPath, leaf); err != nil {
								t.Fatal(err)
							}
							replacement, err = os.Lstat(leaf)
							if err != nil {
								t.Fatal(err)
							}
						case "replacement", "escaping-alias":
							if err := os.Rename(leaf, retained); err != nil {
								t.Fatal(err)
							}
							var err error
							retainedInfo, err = os.Lstat(retained)
							if err != nil {
								t.Fatal(err)
							}
							if mutation == "replacement" {
								if err := os.WriteFile(leaf, []byte("replacement user bytes\n"), 0o600); err != nil {
									t.Fatal(err)
								}
							} else if err := os.Symlink(externalPath, leaf); err != nil {
								t.Fatal(err)
							}
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
					var report TargetReport
					if commandLane {
						report, err = syncCommandFiles(dir, nil, dryRun, true, boundary)
					} else {
						report, err = syncSkillLinks(dir, nil, dryRun, true, portableSyncContext{boundary: boundary})
					}
					if mutation == "hardlink" {
						if hit {
							t.Fatal("pre-existing hardlink reached the marker-read seam instead of staying unowned")
						}
					} else if !hit {
						t.Fatal("captured marker-read seam not reached")
					}
					wantErr := map[string]error{"hardlink-after-enumeration": fsboundary.ErrUnsafeFile, "replacement": fsboundary.ErrGenerationConflict, "escaping-alias": fsboundary.ErrUnsafePath}[mutation]
					if wantErr != nil {
						if !errors.Is(err, wantErr) || len(report.Removed) != 0 {
							t.Fatalf("snapshot refusal became non-ownership/success: %+v %v, want %v", report, err, wantErr)
						}
						assertNativePruneFileSnapshot(t, managedPath, marked, managedInfo)
						switch mutation {
						case "hardlink-after-enumeration":
							assertNativePruneFileSnapshot(t, leaf, marked, replacement)
						case "replacement":
							assertNativePruneFileSnapshot(t, leaf, []byte("replacement user bytes\n"), replacement)
							assertNativePruneFileSnapshot(t, retained, marked, retainedInfo)
						case "escaping-alias":
							text, linkErr := os.Readlink(leaf)
							info, statErr := os.Lstat(leaf)
							if linkErr != nil || statErr != nil || text != externalPath || !os.SameFile(info, replacement) || info.Mode() != replacement.Mode() || !info.ModTime().Equal(replacement.ModTime()) {
								t.Fatalf("refused alias changed: %q %v %v", text, linkErr, statErr)
							}
							assertNativePruneFileSnapshot(t, retained, marked, retainedInfo)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						want := []string{"a-probe.md", "b-managed.md"}
						if mutation == "disappearance" || mutation == "hardlink" {
							want = []string{"b-managed.md"}
						}
						if mutation == "hardlink" {
							assertNativePruneFileSnapshot(t, leaf, marked, probeInfo)
						}
						if !slices.Equal(report.Removed, want) {
							t.Fatalf("healthy removals=%v want=%v", report.Removed, want)
						}
						for _, name := range want {
							path := filepath.Join(dir, name)
							if dryRun {
								if data, err := os.ReadFile(path); err != nil || string(data) != string(marked) {
									t.Fatalf("dry-run changed managed file: %q %v", data, err)
								}
							} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
								t.Fatalf("healthy managed file retained: %s %v", name, err)
							}
						}
					}
					assertNativePruneFileSnapshot(t, externalPath, marked, externalInfo)
					assertNativePruneFileSnapshot(t, userPath, []byte("ordinary handwritten file\n"), userInfo)
					entries, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					names := make([]string, 0, len(entries))
					for _, entry := range entries {
						names = append(names, entry.Name())
					}
					wantNames := []string{"a-probe.md", "b-managed.md", "c-user.md"}
					if wantErr == nil && !dryRun {
						wantNames = []string{"c-user.md"}
						if mutation == "hardlink" {
							wantNames = []string{"a-probe.md", "c-user.md"}
						}
					} else if mutation == "disappearance" {
						wantNames = []string{"b-managed.md", "c-user.md"}
					}
					if !slices.Equal(names, wantNames) {
						t.Fatalf("unexpected prune/staging residue: names=%v want=%v", names, wantNames)
					}
				})
			}
		}
	}
}

// spec: materialization/prune-directory-refusal-precedes-foreign-discovery
func TestPruneDirectorySkipsClassificationWithoutPruning(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprint("dry-run=", dryRun), func(t *testing.T) {
			root, external := t.TempDir(), t.TempDir()
			dir := filepath.Join(root, "native")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			marked := []byte("---\n_managed_by: agent-marketplace\n---\n\ninert marked payload\n")
			externalPath := filepath.Join(external, "external.md")
			if err := os.WriteFile(externalPath, marked, 0o640); err != nil {
				t.Fatal(err)
			}
			leaf := filepath.Join(dir, "a-linked.md")
			if err := os.Link(externalPath, leaf); err != nil {
				t.Fatal(err)
			}
			probeInfo, err := os.Lstat(leaf)
			if err != nil {
				t.Fatal(err)
			}
			boundary := &destinationBoundary{repoRoot: root, roots: []string{dir}}
			classified := false
			boundary.pruneMarkerReadHook = func(string) { classified = true }
			report, err := syncSkillLinks(dir, nil, dryRun, false, portableSyncContext{boundary: boundary})
			if err != nil {
				t.Fatalf("hardlinked marker blocked a sync that does not prune: %v", err)
			}
			if classified {
				t.Fatal("marker classification ran while pruning was disabled")
			}
			if len(report.Removed) != 0 {
				t.Fatalf("pruning-disabled sync reported removals: %v", report.Removed)
			}
			assertNativePruneFileSnapshot(t, leaf, marked, probeInfo)
			assertNativePruneFileSnapshot(t, externalPath, marked, probeInfo)
		})
	}
}

// spec: materialization/prune-directory-refusal-precedes-foreign-discovery
func TestRunPruneDirectoryMarkerRefusalHasNativeDiagnostic(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprint("dry-run=", dryRun), func(t *testing.T) {
			home, repo, external := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			selected := resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "selected", Resource: ir.NewCommand("selected", "prune", "selected inert body", nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}
			initial, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{selected})
			if err != nil || diag.HasErrors(initial.Diags) {
				t.Fatalf("native install control: %+v %v", initial, err)
			}
			selectedPath := filepath.Join(repo, ".factory", "commands", "selected.md")
			selectedBytes, err := os.ReadFile(selectedPath)
			if err != nil {
				t.Fatal(err)
			}
			selectedInfo, err := os.Lstat(selectedPath)
			if err != nil {
				t.Fatal(err)
			}
			externalPath := filepath.Join(external, "external.md")
			data := []byte("---\n_managed_by: agent-marketplace\n---\n\nexternal inert body\n")
			if err := os.WriteFile(externalPath, data, 0o640); err != nil {
				t.Fatal(err)
			}
			leaf := filepath.Join(repo, ".factory", "commands", "a-hardlinked.md")
			if err := os.Link(externalPath, leaf); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(leaf)
			if err != nil {
				t.Fatal(err)
			}
			report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{selected}, func(cfg *runConfig) { cfg.dryRun = dryRun })
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("known multiply linked marker blocked the native repeat: %+v %v", report, err)
			}
			for _, tr := range report.TargetReports() {
				if len(tr.Removed) != 0 {
					t.Fatalf("known hardlink was reported removed: %+v", tr)
				}
			}
			assertNativePruneFileSnapshot(t, leaf, data, info)
			assertNativePruneFileSnapshot(t, externalPath, data, info)
			assertNativePruneFileSnapshot(t, selectedPath, selectedBytes, selectedInfo)
		})
	}
}

func TestCommandMarkerReadRefusesVanishedDirectory(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%v", dryRun), func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "commands")
			links := []skillLink{{name: "probe.md", artifacts: []artifact.Artifact{{Path: "probe.md", Bytes: []byte("---\ndescription: inert\n---\n\nfirst inert body"), Mode: 0o644}}}}
			boundary := &destinationBoundary{repoRoot: root, roots: []string{dir}}
			if _, err := syncCommandFiles(dir, links, false, false, boundary); err != nil {
				t.Fatal(err)
			}
			retained := filepath.Join(root, "retained-commands")
			moved := false
			commandMarkerReadHook = func(path string) {
				if moved || filepath.Dir(path) != dir {
					return
				}
				moved = true
				if err := os.Rename(dir, retained); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { commandMarkerReadHook = nil })
			links[0].artifacts[0].Bytes = []byte("---\ndescription: inert\n---\n\nsecond inert body")
			report, err := syncCommandFiles(dir, links, dryRun, false, boundary)
			if !moved {
				t.Fatal("marker read seam not reached")
			}
			if err == nil || hasDiagCode(report.Diags, "materialize.collision.user-owned") {
				t.Fatalf("vanished command directory reported as a user-owned collision: %+v %v", report, err)
			}
			if data, readErr := os.ReadFile(filepath.Join(retained, "probe.md")); readErr != nil || !strings.Contains(string(data), "first inert body") {
				t.Fatalf("retained command changed: %q %v", data, readErr)
			}
		})
	}
}
