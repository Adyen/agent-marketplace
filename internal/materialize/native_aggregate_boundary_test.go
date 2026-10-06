package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/native-marker-aggregate-refuses-replacement
func TestRunNativeMarkerAggregateRefusesReplacedLeaf(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "repository", true: "machine"}[machine], func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			source, file := config.RepoCommitted, filepath.Join(repo, ".mcp.json")
			if machine {
				source, file = config.UserPersonal, filepath.Join(home, ".claude.json")
			}
			if err := os.WriteFile(file, []byte("{\n // handwritten comment\n \"custom\": true,\n \"mcpServers\": {\"user\": {\"command\": \"inert\"}}\n}\n"), 0o640); err != nil {
				t.Fatal(err)
			}
			desired := nativeMCPResources(hcc.TargetClaude, source)
			initial, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(initial.Diags) {
				t.Fatalf("native aggregate install failed: %+v %v", initial, err)
			}
			desired[0].Resource = ir.NewMcpServer("first", "inert-update", []string{"updated"}, nil, nil, ir.SourceRef{})
			updated, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(updated.Diags) {
				t.Fatalf("native aggregate positive update failed: %+v %v", updated, err)
			}
			before, err := os.ReadFile(file)
			if err != nil || !bytes.Contains(before, []byte("inert-update")) || !bytes.Contains(before, []byte("handwritten comment")) {
				t.Fatalf("aggregate positive control absent: %q %v", before, err)
			}
			document := nativeMCPDocument(t, before)
			if document["custom"] != true || document["mcpServers"].(map[string]any)["user"].(map[string]any)["command"] != "inert" {
				t.Fatalf("native aggregate lost handwritten settings: %v", document)
			}
			beforeInfo, err := os.Stat(file)
			if err != nil || beforeInfo.Mode().Perm() != 0o640 {
				t.Fatalf("aggregate update mode changed: %v", err)
			}
			noop, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(noop.Diags) {
				t.Fatalf("aggregate repeat failed: %+v %v", noop, err)
			}
			if after, err := os.Stat(file); err != nil || !os.SameFile(beforeInfo, after) || beforeInfo.Mode() != after.Mode() || !beforeInfo.ModTime().Equal(after.ModTime()) {
				t.Fatalf("aggregate no-op metadata changed: %v", err)
			}
			desired[0].Resource = ir.NewMcpServer("first", "next-inert-update", nil, nil, nil, ir.SourceRef{})
			retained := filepath.Join(t.TempDir(), "retained.json")
			hit := false
			report, err := Run(t.Context(), repo, desired, func(cfg *runConfig) {
				cfg.linkPublicationHook = func(path string) {
					if path != file {
						return
					}
					hit = true
					if err := os.Rename(file, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte("{\"foreign\":\"inert user bytes\"}\n"), 0o640); err != nil {
						t.Fatal(err)
					}
				}
			})
			if !hit {
				t.Fatal("actual native aggregate publication fixture did not run")
			}
			if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
				t.Fatalf("native aggregate generation replaced without refusal: %+v %v", report, err)
			}
			if data, err := os.ReadFile(file); err != nil || string(data) != "{\"foreign\":\"inert user bytes\"}\n" {
				t.Fatalf("native aggregate replaced foreign bytes: %q %v", data, err)
			}
			if data, err := os.ReadFile(retained); err != nil || !bytes.Equal(data, before) {
				t.Fatalf("admitted aggregate changed: %q %v", data, err)
			}
		})
	}
}

// spec: materialization/native-marker-aggregate-refuses-replacement
func TestRunNativeAggregateEmptyDesiredRefusesUnsafeInventory(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		for _, machine := range []bool{false, true} {
			for _, dryRun := range []bool{false, true} {
				for _, shape := range []string{"hardlink", "symlink", "dangling", "directory"} {
					t.Run(string(target)+"/"+map[bool]string{false: "repository", true: "machine"}[machine]+"/"+map[bool]string{false: "prune", true: "check"}[dryRun]+"/"+shape, func(t *testing.T) {
						repo, file, desired := nativeAggregateInventoryFixture(t, target, machine)
						installed, err := Run(t.Context(), repo, desired)
						if err != nil || diag.HasErrors(installed.Diags) {
							t.Fatalf("native installation control: %+v %v", installed, err)
						}
						before, err := os.ReadFile(file)
						if err != nil || !bytes.Contains(before, []byte(`"_managed_by"`)) {
							t.Fatalf("managed native control missing: %q %v", before, err)
						}
						retained := filepath.Join(t.TempDir(), "retained.json")
						if shape == "hardlink" {
							if err := os.Link(file, retained); err != nil {
								t.Fatal(err)
							}
						} else {
							if err := os.Rename(file, retained); err != nil {
								t.Fatal(err)
							}
							switch shape {
							case "symlink":
								err = os.Symlink(retained, file)
							case "dangling":
								err = os.Symlink(filepath.Join(t.TempDir(), "absent.json"), file)
							case "directory":
								err = os.Mkdir(file, 0o750)
							}
							if err != nil {
								t.Fatal(err)
							}
						}
						retainedInfo, err := os.Stat(retained)
						if err != nil {
							t.Fatal(err)
						}
						leafInfo, err := os.Lstat(file)
						if err != nil {
							t.Fatal(err)
						}
						outputs := make(map[outputKey]bool)
						addExistingNativeAggregateKeys(repo, outputs)
						if !outputs[outputKey{target: target, machineScope: machine}] {
							t.Error("native discovery omitted refused inventory")
						}
						var options []RunOption
						if dryRun {
							options = append(options, WithDryRun())
						}
						report, err := Run(t.Context(), repo, nil, options...)
						if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") || report.InSync() {
							t.Errorf("unsafe native inventory was reported as empty success: %+v %v", report, err)
						}
						assertNativeAggregateNoSuccess(t, report, target, file)
						assertNativeAggregateFilePreserved(t, retained, before, retainedInfo)
						assertNativeAggregateMetadataPreserved(t, file, leafInfo)
						if residue, err := filepath.Glob(filepath.Join(filepath.Dir(file), ".agent-marketplace-*")); err != nil || len(residue) != 0 {
							t.Fatalf("refusal staging residue: %v %v", residue, err)
						}
					})
				}
			}
		}
	}
}

func TestRunNativeAggregateEmptyDesiredControls(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		for _, machine := range []bool{false, true} {
			t.Run(string(target)+"/"+map[bool]string{false: "repository", true: "machine"}[machine], func(t *testing.T) {
				repo, file, desired := nativeAggregateInventoryFixture(t, target, machine)
				absent, err := Run(t.Context(), repo, nil, WithDryRun())
				if err != nil || !absent.InSync() || diag.HasErrors(absent.Diags) {
					t.Fatalf("healthy absent aggregate: %+v %v", absent, err)
				}
				if _, err := os.Lstat(file); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("absent aggregate was created: %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				manual := []byte("{\n // preserved handwritten comment\n \"custom\": true, \"mcpServers\": {\"user\": {\"command\": \"inert\"}}\n}\n")
				if err := os.WriteFile(file, manual, 0o640); err != nil {
					t.Fatal(err)
				}
				manualInfo, err := os.Stat(file)
				if err != nil {
					t.Fatal(err)
				}
				noop, err := Run(t.Context(), repo, nil)
				if err != nil || !noop.InSync() || diag.HasErrors(noop.Diags) {
					t.Fatalf("healthy handwritten no-op: %+v %v", noop, err)
				}
				assertNativeAggregateFilePreserved(t, file, manual, manualInfo)
				installed, err := Run(t.Context(), repo, desired)
				if err != nil || diag.HasErrors(installed.Diags) {
					t.Fatalf("native install control: %+v %v", installed, err)
				}
				managed, err := os.ReadFile(file)
				if err != nil || !bytes.Contains(managed, []byte(`"_managed_by"`)) {
					t.Fatalf("native control lacks ownership: %q %v", managed, err)
				}
				managedInfo, err := os.Stat(file)
				if err != nil {
					t.Fatal(err)
				}
				check, err := Run(t.Context(), repo, nil, WithDryRun())
				if err != nil || diag.HasErrors(check.Diags) || check.InSync() {
					t.Fatalf("real stale aggregate check: %+v %v", check, err)
				}
				assertNativeAggregateFilePreserved(t, file, managed, managedInfo)
				pruned, err := Run(t.Context(), repo, nil)
				if err != nil || diag.HasErrors(pruned.Diags) {
					t.Fatalf("real native prune: %+v %v", pruned, err)
				}
				got, err := os.ReadFile(file)
				if err != nil || bytes.Contains(got, []byte(`"_managed_by"`)) || !bytes.Contains(got, []byte("preserved handwritten comment")) {
					t.Fatalf("native prune failed or lost comment: %q %v", got, err)
				}
				doc := nativeMCPDocument(t, got)
				if doc["custom"] != true || doc["mcpServers"].(map[string]any)["user"].(map[string]any)["command"] != "inert" {
					t.Fatalf("prune lost handwritten settings: %v", doc)
				}
				info, err := os.Stat(file)
				if err != nil || info.Mode().Perm() != 0o640 {
					t.Fatalf("prune changed shared mode: %v", err)
				}
				noop, err = Run(t.Context(), repo, nil)
				if err != nil || !noop.InSync() || diag.HasErrors(noop.Diags) {
					t.Fatalf("repeat prune no-op: %+v %v", noop, err)
				}
				assertNativeAggregateFilePreserved(t, file, got, info)
			})
		}
	}
}

func TestRunNativeAggregateInventoryRefusesBadParent(t *testing.T) {
	for _, shape := range []string{"retargeted", "dangling", "not-directory"} {
		t.Run(shape, func(t *testing.T) {
			repo, file, desired := nativeAggregateInventoryFixture(t, hcc.TargetGemini, false)
			installed, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(installed.Diags) {
				t.Fatalf("native installation control: %+v %v", installed, err)
			}
			before, err := os.ReadFile(file)
			if err != nil || !bytes.Contains(before, []byte(`"_managed_by"`)) {
				t.Fatalf("managed native control missing: %q %v", before, err)
			}
			retained := filepath.Join(t.TempDir(), "admitted-gemini")
			if err := os.Rename(filepath.Dir(file), retained); err != nil {
				t.Fatal(err)
			}
			foreign := t.TempDir()
			external := filepath.Join(foreign, "settings.json")
			foreignBytes := []byte("{\"external\":\"handwritten\"}\n")
			if err := os.WriteFile(external, foreignBytes, 0o640); err != nil {
				t.Fatal(err)
			}
			switch shape {
			case "retargeted":
				err = os.Symlink(foreign, filepath.Dir(file))
			case "dangling":
				err = os.Symlink(filepath.Join(foreign, "missing"), filepath.Dir(file))
			case "not-directory":
				err = os.WriteFile(filepath.Dir(file), []byte("handwritten parent"), 0o640)
			}
			if err != nil {
				t.Fatal(err)
			}
			retainedFile := filepath.Join(retained, "settings.json")
			retainedInfo, err := os.Stat(retainedFile)
			if err != nil {
				t.Fatal(err)
			}
			externalInfo, err := os.Stat(external)
			if err != nil {
				t.Fatal(err)
			}
			outputs := make(map[outputKey]bool)
			addExistingNativeAggregateKeys(repo, outputs)
			if !outputs[outputKey{target: hcc.TargetGemini}] {
				t.Error("native discovery treated an unsafe parent as genuine absence")
			}
			for _, dryRun := range []bool{true, false} {
				var options []RunOption
				if dryRun {
					options = append(options, WithDryRun())
				}
				report, err := Run(t.Context(), repo, nil, options...)
				if err != nil || !hasDiagCode(report.Diags, "materialize.destination.error") || report.InSync() {
					t.Errorf("unsafe parent reported empty success: %+v %v", report, err)
				}
				assertNativeAggregateNoSuccess(t, report, hcc.TargetGemini, file)
				assertNativeAggregateFilePreserved(t, retainedFile, before, retainedInfo)
				assertNativeAggregateFilePreserved(t, external, foreignBytes, externalInfo)
			}
		})
	}
}

func TestNativeAggregateEmptyDesiredGuardsRefuseHardlinks(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			repo, file, desired := nativeAggregateInventoryFixture(t, target, false)
			installed, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(installed.Diags) {
				t.Fatalf("native installation control: %+v %v", installed, err)
			}
			if err := os.Link(file, filepath.Join(t.TempDir(), "external.json")); err != nil {
				t.Fatal(err)
			}
			destinations, err := harness.ResolveDestinations(target, false, repo)
			if err != nil {
				t.Fatal(err)
			}
			for _, destination := range destinations {
				if len(destination.AggregateRoutes) == 0 {
					continue
				}
				empty := map[string]json.RawMessage{"mcpServers": json.RawMessage(`{}`), "hooks": json.RawMessage(`{}`)}
				key := outputKey{target: target}
				var report TargetReport
				if target == hcc.TargetGemini {
					report, _, err = syncGeminiNativeAggregate(t.Context(), key, destination, empty, true, true)
				} else {
					report, err = syncNativeAggregate(key, destination, destination.AggregateFile, empty, true, true)
				}
				if !errors.Is(err, fsboundary.ErrUnsafeFile) || len(report.Written)+len(report.Removed)+len(report.Skipped) != 0 {
					t.Fatalf("empty-desired guard hid unsafe snapshot: %+v %v", report, err)
				}
			}
		})
	}
}

func TestRunNativeAggregateInventoryFIFO(t *testing.T) {
	if os.Getenv("AM_TEST_NATIVE_INVENTORY_FIFO") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunNativeAggregateInventoryFIFO$")
		cmd.Env = append(os.Environ(), "AM_TEST_NATIVE_INVENTORY_FIFO=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bounded native FIFO inventory probe: %v\n%s", err, output)
		}
		return
	}
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			repo, file, desired := nativeAggregateInventoryFixture(t, target, false)
			installed, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(installed.Diags) {
				t.Fatalf("native install control: %+v %v", installed, err)
			}
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			retained := filepath.Join(t.TempDir(), "retained.json")
			if err := os.Rename(file, retained); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(retained)
			if err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(file, 0o600); err != nil {
				t.Fatal(err)
			}
			fifoInfo, err := os.Lstat(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, dryRun := range []bool{true, false} {
				report, err := Run(t.Context(), repo, nil, func(c *runConfig) { c.dryRun = dryRun })
				if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") || report.InSync() {
					t.Errorf("native FIFO inventory reported absence: %+v %v", report, err)
				}
				assertNativeAggregateNoSuccess(t, report, target, file)
				assertNativeAggregateFilePreserved(t, retained, before, info)
				assertNativeAggregateMetadataPreserved(t, file, fifoInfo)
			}
		})
	}
}

func TestRunNativeAggregateInventoryRefusalKeepsIndependentSkill(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			repo, file, desired := nativeAggregateInventoryFixture(t, target, false)
			installed, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(installed.Diags) {
				t.Fatalf("native install control: %+v %v", installed, err)
			}
			retained := filepath.Join(t.TempDir(), "external.json")
			if err := os.Link(file, retained); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			skill := desired[0]
			skill.Name, skill.Kind = "safe-partition", ir.KindSkill
			skill.Resource = ir.NewSkill("safe-partition", "safe", "inert independent skill", nil, nil, ir.SourceRef{})
			report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{skill}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}))
			if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
				t.Fatalf("native inventory refusal missing: %+v %v", report, err)
			}
			skillFile := filepath.Join(repo, "."+string(target), "skills", "safe-partition", "SKILL.md")
			if data, err := os.ReadFile(skillFile); err != nil || !bytes.Contains(data, []byte("inert independent skill")) {
				t.Fatalf("independent safe partition did not materialize: %q %v", data, err)
			}
			assertNativeAggregateNoSuccess(t, report, target, file)
			assertNativeAggregateFilePreserved(t, file, before, info)
			assertNativeAggregateFilePreserved(t, retained, before, info)
		})
	}
}

func TestNativeAggregateSnapshotDisappearanceIsGenerationConflict(t *testing.T) {
	file := filepath.Join(t.TempDir(), "native.json")
	if err := os.WriteFile(file, []byte(`{"_managed_by":"agent-marketplace"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(filepath.Dir(file), "retained.json")
	if err := os.Rename(file, retained); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeCommandFile(file, observed, nil); !errors.Is(err, fsboundary.ErrGenerationConflict) || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("disappeared observed leaf was treated as genuine absence: %v", err)
	}
	assertNativeAggregateFilePreserved(t, retained, []byte(`{"_managed_by":"agent-marketplace"}`), observed)
}

func TestNativeAggregateSnapshotParentDisappearanceIsGenerationConflict(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "native")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "native.json")
	data := []byte(`{"_managed_by":"agent-marketplace"}`)
	if err := os.WriteFile(file, data, 0o640); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readNativeCommandFile(file, observed, nil); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("healthy observed snapshot failed: %q %v", got, err)
	}
	retained := filepath.Join(base, "retained")
	if err := os.Rename(root, retained); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeCommandFile(file, observed, nil); !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("disappeared observed parent was treated as genuine absence: %v", err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("snapshot recreated its disappeared parent: %v", err)
	}
	assertNativeAggregateFilePreserved(t, filepath.Join(retained, "native.json"), data, observed)
}

func nativeAggregateInventoryFixture(t *testing.T, target hcc.Target, machine bool) (string, string, []resolver.ResolvedArtifact) {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	source, root, rel := config.RepoCommitted, repo, ".mcp.json"
	if machine {
		source, root, rel = config.UserPersonal, home, ".claude.json"
	}
	if target == hcc.TargetGemini {
		rel = filepath.Join(".gemini", "settings.json")
	}
	desired := nativeMCPResources(target, source)
	if target == hcc.TargetGemini {
		desired = desired[2:]
	}
	return repo, filepath.Join(root, rel), desired
}

func assertNativeAggregateMetadataPreserved(t *testing.T, file string, before os.FileInfo) {
	t.Helper()
	after, err := os.Lstat(file)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("native aggregate metadata changed at %s: %v", file, err)
	}
}

func assertNativeAggregateFilePreserved(t *testing.T, file string, before []byte, info os.FileInfo) {
	t.Helper()
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("native aggregate bytes changed at %s: %q %v", file, after, err)
	}
	assertNativeAggregateMetadataPreserved(t, file, info)
}

func assertNativeAggregateNoSuccess(t *testing.T, report *Report, target hcc.Target, file string) {
	t.Helper()
	for _, root := range report.PerRoot {
		if root.Target != target {
			continue
		}
		for _, entries := range [][]string{root.Written, root.Removed, root.Skipped} {
			for _, rel := range entries {
				if filepath.Join(root.OutDir, rel) == file {
					t.Fatalf("refused aggregate reported successful: %+v", root)
				}
			}
		}
	}
}
