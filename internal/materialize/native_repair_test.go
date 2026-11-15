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

func nativeRepairPluginSelection(t *testing.T, target hcc.Target, scopes []config.Source, sharedHome bool, includeMCP bool) (string, []resolver.ResolvedArtifact) {
	t.Helper()
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	home := filepath.Join(root, "home")
	if sharedHome {
		home = repoRoot
	}
	for _, dir := range []string{repoRoot, home, filepath.Join(root, "state"), filepath.Join(root, "cache")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))

	packageRoot := filepath.Join(root, "plugin")
	hooks := []*ir.Hook{
		ir.NewHookWithMatcher("repo-guard", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"}),
		ir.NewHookWithMatcher("machine-guard", "PostToolUse", "Write", "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"}),
	}
	server := ir.NewMcpServer("plugin-tools", "node", nil, nil, nil, ir.SourceRef{File: ".mcp.json"})
	files := []ir.PluginFile{
		writePluginPackageFile(t, packageRoot, ".claude-plugin/plugin.json", `{"name":"repair-kit"}`, 0o644),
		writePluginPackageFile(t, packageRoot, "hooks/hooks.json", `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}],"PostToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}`, 0o644),
		writePluginPackageFile(t, packageRoot, "scripts/check.sh", "#!/bin/sh\necho check\n", 0o755),
		writePluginPackageFile(t, packageRoot, ".mcp.json", `{"mcpServers":{"plugin-tools":{"command":"node"}}}`, 0o644),
	}
	plugin := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "repair-kit"},
		Members:  []ir.PluginMember{{Resource: hooks[0]}, {Resource: hooks[1]}, {Resource: server}},
		Files:    files,
	}
	resolved := func(source config.Source, memberPath string, resource ir.Resource) resolver.ResolvedArtifact {
		name := resource.Name()
		pkg := &resolver.PluginPackage{
			Name:               "repair-kit",
			PackageRoot:        packageRoot,
			CatalogFingerprint: "test-catalog",
			Delivery:           "decompose",
			Scope:              source.String(),
			MemberPath:         memberPath,
			Package:            plugin,
		}
		return resolver.ResolvedArtifact{
			Entry:         catalog.Entry{Kind: resource.Kind(), Name: name, Path: filepath.Join(packageRoot, filepath.FromSlash(memberPath)), Resource: resource},
			RegistryURL:   packageRoot,
			V4Kind:        config.V4SourceDirectory,
			Source:        source,
			Harnesses:     []hcc.Target{target},
			InstalledName: "repair-kit-" + name,
			PluginPackage: pkg,
		}
	}
	selection := make([]resolver.ResolvedArtifact, 0, len(scopes)+1)
	for _, source := range scopes {
		hook := hooks[0]
		if source.IsMachineScope() {
			hook = hooks[1]
		}
		selection = append(selection, resolved(source, "hooks/hooks.json", hook))
	}
	if includeMCP {
		selection = append(selection, resolved(config.RepoCommitted, ".mcp.json", server))
	}
	selection, generationDiags := preparePluginGenerations(selection, repoRoot, false, false, nil)
	if hasErrorDiagnostics(generationDiags) {
		t.Fatalf("prepare plugin generation: %+v", generationDiags)
	}
	report, err := Run(context.Background(), repoRoot, selection, WithPluginStateReconciliation())
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("materialize selected plugin: %v %+v", err, report)
	}
	return repoRoot, selection
}

func assertPluginRepairEntries(t *testing.T, file *RepairFile, kind ownership.EntryKind, resources []string) {
	t.Helper()
	if len(file.Adoptions) != len(resources) || len(file.Conflicts) != 0 || len(file.Orphans) != 0 {
		evidence := ""
		if len(file.Orphans) > 0 {
			evidence = file.Orphans[0].Evidence()
		}
		t.Fatalf("selected plugin entries: adopt=%d replace=%d orphan=%d evidence=%q", len(file.Adoptions), len(file.Conflicts), len(file.Orphans), evidence)
	}
	got := make(map[string]bool, len(resources))
	for _, candidate := range file.Adoptions {
		id := candidate.Candidate.EntryID()
		if id.Kind != kind {
			t.Fatalf("plugin adoption kind = %q, want %q", id.Kind, kind)
		}
		got[candidate.Resource] = true
	}
	for _, resource := range resources {
		if !got[resource] {
			t.Fatalf("missing selected plugin adoption for %q", resource)
		}
	}
}

// spec: doctor-repair-planning/native-aggregate-repair-retains-action-safety
func TestNativeAggregateRepairIncludesSelectedPluginDesiredEntries(t *testing.T) {
	t.Run("Claude hook and marker MCP fragment", func(t *testing.T) {
		repoRoot, selection := nativeRepairPluginSelection(t, hcc.TargetClaude, []config.Source{config.RepoCommitted}, false, true)
		key := outputKey{target: hcc.TargetClaude}
		destination, hookSpec, err := nativeHookJSONLayout(key, repoRoot)
		if err != nil {
			t.Fatal(err)
		}
		hookPath := filepath.Join(destination.Root, hookSpec.rel)
		hookBefore, err := os.ReadFile(hookPath)
		if err != nil {
			t.Fatal(err)
		}
		hookInfo, err := os.Stat(hookPath)
		if err != nil {
			t.Fatal(err)
		}
		mcpRoot, mcpRel, mcpContainer := nativeRepairMCPFile(t, repoRoot, hcc.TargetClaude, false)
		mcpPath := filepath.Join(mcpRoot, mcpRel)
		mcpBefore, err := os.ReadFile(mcpPath)
		if err != nil {
			t.Fatal(err)
		}
		mcpInfo, err := os.Stat(mcpPath)
		if err != nil {
			t.Fatal(err)
		}
		var mcpDocument struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(mcpBefore, &mcpDocument); err != nil {
			t.Fatal(err)
		}
		if _, ok := mcpDocument.Servers["repair-kit-plugin-tools"]; !ok {
			t.Fatalf("Run did not materialize the selected plugin MCP fragment: %s", mcpBefore)
		}
		statePath := removeNativeRepairState(t, destination.Root)
		mcpStatePath := ""
		if filepath.Clean(mcpRoot) != filepath.Clean(destination.Root) {
			mcpStatePath = removeNativeRepairState(t, mcpRoot)
		}
		previews, err := RepairPreviews(context.Background(), repoRoot, selection, nil)
		if err != nil {
			t.Fatal(err)
		}
		mcpFile := nativeRepairFileFor(t, previews, hcc.TargetClaude, mcpRoot, mcpRel, mcpContainer)
		if len(mcpFile.Adoptions)+len(mcpFile.Conflicts) != 1 || len(mcpFile.Orphans) != 0 {
			t.Fatalf("selected plugin MCP entry: adopt=%d replace=%d orphan=%d", len(mcpFile.Adoptions), len(mcpFile.Conflicts), len(mcpFile.Orphans))
		}
		mcpID := ownership.EntryID{}
		mcpResource := ""
		if len(mcpFile.Adoptions) == 1 {
			mcpID = mcpFile.Adoptions[0].Candidate.EntryID()
			mcpResource = mcpFile.Adoptions[0].Resource
		} else {
			mcpID = mcpFile.Conflicts[0].Candidate.EntryID()
			mcpResource = mcpFile.Conflicts[0].Resource
		}
		if mcpID.Kind != ownership.KindMcpServer || mcpID.Key != "repair-kit-plugin-tools" || mcpResource != "repair-kit-plugin-tools" {
			t.Fatalf("selected plugin MCP entry = %+v resource=%q", mcpID, mcpResource)
		}
		assertPluginRepairEntries(t, nativeRepairFileFor(t, previews, hcc.TargetClaude, destination.Root, hookSpec.rel, hookSpec.container), ownership.KindHook, []string{"repair-kit-repo-guard"})
		assertNativeRepairSnapshot(t, hookPath, hookBefore, hookInfo.Mode(), hookInfo.ModTime())
		assertNativeRepairSnapshot(t, mcpPath, mcpBefore, mcpInfo.Mode(), mcpInfo.ModTime())
		if _, err := os.Stat(statePath); !os.IsNotExist(err) {
			t.Fatalf("preview changed missing ownership state: %v", err)
		}
		if mcpStatePath != "" {
			if _, err := os.Stat(mcpStatePath); !os.IsNotExist(err) {
				t.Fatalf("preview changed missing MCP ownership state: %v", err)
			}
		}
	})

	t.Run("Gemini coalesced scopes", func(t *testing.T) {
		repoRoot, selection := nativeRepairPluginSelection(t, hcc.TargetGemini, []config.Source{config.RepoCommitted, config.UserPersonal}, true, false)
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
		statePath := removeNativeRepairState(t, destination.Root)
		previews, err := RepairPreviews(context.Background(), repoRoot, selection, nil)
		if err != nil {
			t.Fatal(err)
		}
		file := nativeRepairFileFor(t, previews, hcc.TargetGemini, destination.Root, hookSpec.rel, hookSpec.container)
		assertPluginRepairEntries(t, file, ownership.KindHook, []string{"repair-kit-repo-guard", "repair-kit-machine-guard"})
		assertNativeRepairSnapshot(t, path, before, info.Mode(), info.ModTime())
		if _, err := os.Stat(statePath); !os.IsNotExist(err) {
			t.Fatalf("preview changed missing ownership state: %v", err)
		}
	})
}
