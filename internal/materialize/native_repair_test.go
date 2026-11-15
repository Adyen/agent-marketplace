package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type nativeRepairCase struct {
	target  hcc.Target
	machine bool
}

var nativeRepairCases = []nativeRepairCase{
	{target: hcc.TargetClaude},
	{target: hcc.TargetFactory, machine: true},
	{target: hcc.TargetCursor},
	{target: hcc.TargetWindsurf, machine: true},
	{target: hcc.TargetGemini},
	{target: hcc.TargetOpenCode, machine: true},
}

func nativeRepairEnvironment(t *testing.T) (string, string) {
	t.Helper()
	repoRoot, home := devinEnv(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	return repoRoot, home
}

func nativeRepairSelection(target hcc.Target, name, command string, machine bool) resolver.ResolvedArtifact {
	source := config.RepoCommitted
	if machine {
		source = config.UserPersonal
	}
	resource := ir.NewMcpServer(name, command, nil, nil, nil, ir.SourceRef{})
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: name, Resource: resource},
		Source:    source,
		Harnesses: []hcc.Target{target},
	}
}

func nativeRepairMCPFile(t *testing.T, repoRoot string, target hcc.Target, machine bool) (string, string, string) {
	t.Helper()
	if target == hcc.TargetGemini {
		destination, file, err := nativeHookJSONLayout(outputKey{target: target, machineScope: machine}, repoRoot)
		if err != nil {
			t.Fatalf("resolve Gemini settings destination: %v", err)
		}
		return destination.Root, file.rel, "mcpServers"
	}
	declaration, ok := targetmeta.NativeAggregateFileForScope(targetmeta.Target(target), ir.KindMcpServer, machine)
	if !ok {
		t.Fatalf("%s has no declared MCP aggregate", target)
	}
	destinations, err := harness.ResolveDestinations(target, machine, repoRoot)
	if err != nil {
		t.Fatalf("resolve %s destinations: %v", target, err)
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			for _, route := range destination.AggregateRoutes {
				if route.Container == declaration.Member && filepath.Base(route.ArtifactPath) == filepath.Base(declaration.Path) {
					return destination.Root, destination.AggregateFile, route.Container
				}
			}
		}
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			continue
		}
		rel, err := destination.RouteArtifact(declaration.Path)
		if err == nil {
			return destination.Root, rel, declaration.Member
		}
	}
	t.Fatalf("%s MCP aggregate %q has no resolved destination", target, declaration.Path)
	return "", "", ""
}

func nativeRepairFileFor(t *testing.T, previews []TargetRepairPreview, target hcc.Target, root, rel string, containers ...string) *RepairFile {
	t.Helper()
	for _, targetPreview := range previews {
		if targetPreview.Target != target || targetPreview.Preview == nil {
			continue
		}
		for _, file := range targetPreview.Preview.Files {
			if file.OutDir == root && file.Rel == rel {
				if len(containers) > 0 {
					adapter, ok := file.adapter.(ownedJSONRepairAdapter)
					if !ok || adapter.file.container != containers[0] {
						continue
					}
				}
				return file
			}
		}
	}
	t.Fatalf("no repair file for %s at %s/%s", target, root, rel)
	return nil
}

func nativeRepairTargetPreview(t *testing.T, previews []TargetRepairPreview, target hcc.Target) *RepairPreview {
	t.Helper()
	for _, targetPreview := range previews {
		if targetPreview.Target == target {
			return targetPreview.Preview
		}
	}
	t.Fatalf("no repair preview for %s", target)
	return nil
}

func prepareNativeRepairFile(t *testing.T, target hcc.Target, machine bool) (string, string, string, string, []resolver.ResolvedArtifact) {
	t.Helper()
	repoRoot, _ := nativeRepairEnvironment(t)
	root, rel, container := nativeRepairMCPFile(t, repoRoot, target, machine)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	document := map[string]any{
		container: map[string]any{"handwritten": map[string]any{"command": "handwritten-binary"}},
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, rel)
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "repair-" + string(target)
	selection := []resolver.ResolvedArtifact{nativeRepairSelection(target, name, "desired-"+name, machine)}
	report, err := Run(context.Background(), repoRoot, selection)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed %s aggregate: %v %+v", target, err, report)
	}
	return repoRoot, root, rel, container, selection
}

func prepareNativeRepairOrphan(t *testing.T, tc nativeRepairCase) (string, string, string, string, []resolver.ResolvedArtifact) {
	t.Helper()
	if tc.target == hcc.TargetOpenCode {
		return prepareNativeRepairFile(t, tc.target, tc.machine)
	}
	repoRoot, _ := nativeRepairEnvironment(t)
	source := config.RepoCommitted
	if tc.machine {
		source = config.UserPersonal
	}
	name := "repair-" + string(tc.target) + "-orphan"
	command := "echo native-repair-" + string(tc.target)
	definition := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + command + `"}]}]}}`)
	selection, _ := sourceBackedHookFixtures(t, "claude", source, tc.target, name, definition, nil)
	if len(selection) != 1 {
		t.Fatalf("%s hook fixture yielded %d resources", tc.target, len(selection))
	}
	destination, fileSpec, err := nativeHookJSONLayout(outputKey{target: tc.target, machineScope: tc.machine}, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	layoutCase := nativeHookJSONCaseFor(t, tc.target, tc.machine)
	initial, _, _, _ := nativeHookJSONSeed(layoutCase)
	path := filepath.Join(destination.Root, fileSpec.rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), repoRoot, selection)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed %s hook aggregate: %v %+v", tc.target, err, report)
	}
	return repoRoot, destination.Root, fileSpec.rel, command, selection
}

func rewriteNativeRepairValue(t *testing.T, path, container, key string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(document[container], &entries); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(entries[key], &value); err != nil {
		t.Fatal(err)
	}
	value["command"] = "existing-user-value"
	entries[key], err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	document[container], err = json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func nativeRepairStatePath(t *testing.T, root string) string {
	t.Helper()
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return store.StateFile()
}

func removeNativeRepairState(t *testing.T, root string) string {
	t.Helper()
	statePath := nativeRepairStatePath(t, root)
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return statePath
}

func assertNativeRepairSnapshot(t *testing.T, path string, raw []byte, mode os.FileMode, modified time.Time) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, after) || info.Mode().Perm() != mode.Perm() || !info.ModTime().Equal(modified) {
		t.Fatalf("repair preview or refused action changed %s", path)
	}
}

// spec: doctor-repair-planning/native-aggregate-repair-retains-action-safety
func TestNativeAggregateRepairActions(t *testing.T) {
	for _, tc := range nativeRepairCases {
		t.Run(string(tc.target), func(t *testing.T) {
			t.Run("adoption", func(t *testing.T) {
				repoRoot, root, rel, container, selection := prepareNativeRepairFile(t, tc.target, tc.machine)
				path := filepath.Join(root, rel)
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				statePath := removeNativeRepairState(t, root)
				previews, err := RepairPreviews(context.Background(), repoRoot, selection, nil)
				if err != nil {
					t.Fatalf("repair preview: %v", err)
				}
				file := nativeRepairFileFor(t, previews, tc.target, root, rel, container)
				if len(file.Adoptions) != 1 || len(file.Conflicts) != 0 || len(file.Orphans) != 0 {
					t.Fatalf("exact desired entry candidates: adopt=%d replace=%d orphan=%d", len(file.Adoptions), len(file.Conflicts), len(file.Orphans))
				}
				assertNativeRepairSnapshot(t, path, before, info.Mode(), info.ModTime())
				if _, err := os.Stat(statePath); !os.IsNotExist(err) {
					t.Fatalf("preview changed missing ownership state: %v", err)
				}
				if _, err := file.Adopt([]int{0}); err != nil {
					t.Fatalf("adopt exact value: %v", err)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("adoption changed aggregate bytes: %v", err)
				}
			})

			t.Run("confirmed replacement", func(t *testing.T) {
				repoRoot, root, rel, container, selection := prepareNativeRepairFile(t, tc.target, tc.machine)
				path := filepath.Join(root, rel)
				name := selection[0].Name
				if err := os.WriteFile(path, rewriteNativeRepairValue(t, path, container, name), 0o600); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				removeNativeRepairState(t, root)
				previews, err := RepairPreviews(context.Background(), repoRoot, selection, nil)
				if err != nil {
					t.Fatalf("repair preview: %v", err)
				}
				file := nativeRepairFileFor(t, previews, tc.target, root, rel, container)
				if len(file.Adoptions) != 0 || len(file.Conflicts) != 1 || len(file.Orphans) != 0 {
					t.Fatalf("differing desired entry candidates: adopt=%d replace=%d orphan=%d", len(file.Adoptions), len(file.Conflicts), len(file.Orphans))
				}
				if _, err := file.ReplaceConflict(0, false); !errors.Is(err, ownership.ErrConfirmationRequired) {
					t.Fatalf("unconfirmed replacement error = %v", err)
				}
				unchanged, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, unchanged) {
					t.Fatalf("unconfirmed replacement changed aggregate: %v", err)
				}
				result, err := file.ReplaceConflict(0, true)
				if err != nil {
					t.Fatalf("confirmed replacement: %v", err)
				}
				if result.BackupPath == "" {
					t.Fatal("replacement did not create a safety backup")
				}
				backup, err := os.ReadFile(result.BackupPath)
				if err != nil || !bytes.Equal(before, backup) {
					t.Fatalf("replacement backup did not preserve exact prior bytes: %v", err)
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var document map[string]json.RawMessage
				if err := json.Unmarshal(after, &document); err != nil {
					t.Fatal(err)
				}
				var entries map[string]json.RawMessage
				if err := json.Unmarshal(document[container], &entries); err != nil {
					t.Fatal(err)
				}
				var replaced map[string]any
				if err := json.Unmarshal(entries[name], &replaced); err != nil {
					t.Fatal(err)
				}
				command := ""
				switch value := replaced["command"].(type) {
				case string:
					command = value
				case []any:
					if len(value) == 1 {
						command, _ = value[0].(string)
					}
				}
				if command != "desired-"+name {
					t.Fatalf("replacement command = %v", replaced["command"])
				}
				if _, ok := entries["handwritten"]; !ok {
					t.Fatal("replacement removed unrelated handwritten content")
				}
			})

			t.Run("evidence-backed orphan removal", func(t *testing.T) {
				repoRoot, root, rel, orphanNeedle, selection := prepareNativeRepairOrphan(t, tc)
				path := filepath.Join(root, rel)
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				removeNativeRepairState(t, root)
				previews, err := RepairPreviews(context.Background(), repoRoot, nil, nil)
				if err != nil {
					t.Fatalf("orphan preview: %v", err)
				}
				file := nativeRepairFileFor(t, previews, tc.target, root, rel)
				if len(file.Adoptions) != 0 || len(file.Conflicts) != 0 || len(file.Orphans) != 1 {
					t.Fatalf("orphan candidates: adopt=%d replace=%d orphan=%d", len(file.Adoptions), len(file.Conflicts), len(file.Orphans))
				}
				if !strings.Contains(file.Orphans[0].Evidence(), string(ownership.EvidenceSafetyBackup)) {
					t.Fatalf("orphan lacks retained safety-backup evidence: %q", file.Orphans[0].Evidence())
				}
				if _, err := file.RemoveOrphan(0, false); !errors.Is(err, ownership.ErrConfirmationRequired) {
					t.Fatalf("unconfirmed removal error = %v", err)
				}
				unchanged, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, unchanged) {
					t.Fatalf("unconfirmed removal changed aggregate: %v", err)
				}
				result, err := file.RemoveOrphan(0, true)
				if err != nil {
					t.Fatalf("confirmed orphan removal: %v", err)
				}
				if result.BackupPath == "" {
					t.Fatal("orphan removal did not create a safety backup")
				}
				backup, err := os.ReadFile(result.BackupPath)
				if err != nil || !bytes.Equal(before, backup) {
					t.Fatalf("orphan backup did not preserve exact prior bytes: %v", err)
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if tc.target == hcc.TargetOpenCode {
					var document map[string]json.RawMessage
					if err := json.Unmarshal(after, &document); err != nil {
						t.Fatal(err)
					}
					var entries map[string]json.RawMessage
					if err := json.Unmarshal(document["mcp"], &entries); err != nil {
						t.Fatal(err)
					}
					if _, ok := entries[selection[0].Name]; ok {
						t.Fatal("confirmed orphan remains in the aggregate")
					}
					if _, ok := entries["handwritten"]; !ok {
						t.Fatal("orphan removal removed unrelated handwritten content")
					}
				} else if bytes.Contains(after, []byte(orphanNeedle)) || !bytes.Contains(after, []byte(nativeHookJSONManualCommand)) {
					t.Fatal("orphan removal did not preserve unrelated hook content")
				}
			})
		})
	}
}

// spec: doctor-repair-planning/repair-combines-shared-gemini-scopes
func TestGeminiRepairCombinesSharedScopesWithoutDuplicateCandidates(t *testing.T) {
	for _, aliasHome := range []bool{false, true} {
		t.Run(map[bool]string{false: "same path", true: "filesystem alias"}[aliasHome], func(t *testing.T) {
			repoRoot := t.TempDir()
			home := repoRoot
			if aliasHome {
				home = filepath.Join(t.TempDir(), "home-alias")
				if err := os.Symlink(repoRoot, home); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("HOME", home)
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
			selection := sharedGeminiResources(t)
			report, err := Run(context.Background(), repoRoot, selection)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("seed shared Gemini settings: %v %+v", err, report)
			}
			destination, hookSpec, err := nativeHookJSONLayout(outputKey{target: hcc.TargetGemini, machineScope: true}, repoRoot)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(destination.Root, hookSpec.rel)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			store, err := ownership.Open(destination.Root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(store.StateFile()); err != nil {
				t.Fatal(err)
			}
			previews, err := RepairPreviews(context.Background(), repoRoot, selection, nil)
			if err != nil {
				t.Fatalf("repair preview: %v", err)
			}
			preview := nativeRepairTargetPreview(t, previews, hcc.TargetGemini)
			var candidates int
			seen := make(map[string]bool)
			for _, file := range preview.Files {
				if file.AbsFile() != path {
					continue
				}
				for _, candidate := range file.Adoptions {
					id := candidate.Candidate.EntryID()
					key := string(id.Kind) + "\x00" + id.Container + "\x00" + id.Key + "\x00" + id.IdentityDigest
					if seen[key] {
						t.Fatalf("Gemini candidate %q was listed more than once", key)
					}
					seen[key] = true
					candidates++
				}
			}
			if candidates != 4 {
				t.Fatalf("shared Gemini candidates = %d, want two MCP entries and two hook entries", candidates)
			}
			assertNativeRepairSnapshot(t, path, before, info.Mode(), info.ModTime())
			if _, err := os.Stat(store.StateFile()); !os.IsNotExist(err) {
				t.Fatalf("preview changed missing Gemini ownership state: %v", err)
			}
			for range candidates {
				current, err := RepairPreviews(context.Background(), repoRoot, selection, nil)
				if err != nil {
					t.Fatal(err)
				}
				acted := false
				for _, file := range nativeRepairTargetPreview(t, current, hcc.TargetGemini).Files {
					if len(file.Adoptions) == 0 {
						continue
					}
					indices := make([]int, len(file.Adoptions))
					for i := range indices {
						indices[i] = i
					}
					if _, err := file.Adopt(indices); err != nil {
						t.Fatalf("adopt shared Gemini entries: %v", err)
					}
					acted = true
					break
				}
				if !acted {
					break
				}
			}
			report, err = Run(context.Background(), repoRoot, selection)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("sync after Gemini repair: %v %+v", err, report)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var settings map[string]json.RawMessage
			if err := json.Unmarshal(after, &settings); err != nil {
				t.Fatal(err)
			}
			var servers map[string]json.RawMessage
			if err := json.Unmarshal(settings["mcpServers"], &servers); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"repository-mcp", "machine-mcp"} {
				if _, ok := servers[name]; !ok {
					t.Fatalf("shared settings lost MCP entry %q", name)
				}
			}
			if !bytes.Contains(after, []byte("repository-audit")) || !bytes.Contains(after, []byte("machine-audit")) {
				t.Fatal("shared settings lost a scope's hook entry")
			}
		})
	}
}

// spec: doctor-repair-planning/repair-combines-shared-gemini-scopes
func TestGeminiRepairScopeConflictRefusesWithoutMutation(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", repoRoot)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	initial := []resolver.ResolvedArtifact{sharedGeminiMCPResource("initial", config.RepoCommitted)}
	report, err := Run(context.Background(), repoRoot, initial)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Gemini state: %v %+v", err, report)
	}
	destination, hookSpec, err := nativeHookJSONLayout(outputKey{target: hcc.TargetGemini}, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(destination.Root, hookSpec.rel)
	settingsBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(destination.Root)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	repoEntry := sharedGeminiMCPResource("conflict", config.RepoCommitted)
	machineEntry := sharedGeminiMCPResource("conflict", config.UserPersonal)
	name := repoEntry.Name
	machineEntry.Entry = catalog.Entry{
		Kind:     ir.KindMcpServer,
		Name:     name,
		Resource: ir.NewMcpServer(name, "different-machine-value", nil, nil, nil, ir.SourceRef{}),
	}
	previews, err := RepairPreviews(context.Background(), repoRoot, []resolver.ResolvedArtifact{repoEntry, machineEntry}, nil)
	if err != nil {
		t.Fatalf("conflicting Gemini preview: %v", err)
	}
	preview := nativeRepairTargetPreview(t, previews, hcc.TargetGemini)
	if preview.Actionable() || len(preview.Files) != 0 {
		t.Fatalf("conflicting shared Gemini settings offered candidates: %+v", preview.Files)
	}
	found := false
	for _, diagnostic := range preview.Diags {
		if diagnostic.Code == diag.CodeMaterializeDestinationOverlap {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Gemini overlap diagnostic missing: %+v", preview.Diags)
	}
	settingsAfter, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(settingsBefore, settingsAfter) {
		t.Fatalf("Gemini settings changed after conflict refusal: %v", err)
	}
	stateAfter, err := os.ReadFile(store.StateFile())
	if err != nil || !bytes.Equal(stateBefore, stateAfter) {
		t.Fatalf("Gemini ownership state changed after conflict refusal: %v", err)
	}
}
