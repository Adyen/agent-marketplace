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

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func sharedGeminiMCPResource(name string, source config.Source) resolver.ResolvedArtifact {
	resource := ir.NewMcpServer(name+"-mcp", name+"-tool", nil, nil, nil, ir.SourceRef{})
	return resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: resource.Kind(), Name: resource.Name(), Resource: resource}, Source: source, Harnesses: []hcc.Target{hcc.TargetGemini}}
}

func sharedGeminiResources(t testing.TB) []resolver.ResolvedArtifact {
	t.Helper()
	var desired []resolver.ResolvedArtifact
	for _, scope := range []struct {
		name   string
		source config.Source
	}{{"repository", config.RepoCommitted}, {"machine", config.UserPersonal}} {
		desired = append(desired, sharedGeminiMCPResource(scope.name, scope.source))
		hookName := scope.name + "-audit"
		command := "echo " + hookName
		definition := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + command + `"}]}]}}`)
		hooks, _ := sourceBackedHookFixtures(t, "claude", scope.source, hcc.TargetGemini, hookName, definition, nil)
		if len(hooks) != 1 {
			t.Fatalf("Gemini %s hook ingest returned %d resources, want one", scope.name, len(hooks))
		}
		desired = append(desired, hooks[0])
	}
	return desired
}

func geminiSupportHook(t *testing.T, name string, source config.Source, script, payload []byte) (resolver.ResolvedArtifact, string) {
	t.Helper()
	command := "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"
	definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + command + `"}]}]}}`)
	supportFiles := map[string][]byte{
		"hooks/scripts/check.sh":     script,
		"hooks/scripts/payload.json": payload,
	}
	hooks, sourceRoot := sourceBackedHookFixtures(t, "claude", source, hcc.TargetGemini, name, definition, supportFiles)
	if len(hooks) != 1 {
		t.Fatalf("Gemini %s hook ingest returned %d resources, want one", name, len(hooks))
	}
	hook, ok := hooks[0].Resource.(*ir.Hook)
	if !ok || hook.SourceDefinitionDigest() == "" {
		t.Fatalf("Gemini hook is not bound to its actual source definition: %#v", hooks[0].Resource)
	}
	if err := os.Chmod(filepath.Join(sourceRoot, "hooks", "scripts", "check.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	return hooks[0], sourceRoot
}

func assertGeminiSupportArtifact(t *testing.T, path string, wantSymlink bool, contents []byte, mode os.FileMode) (os.FileInfo, string, os.FileInfo) {
	t.Helper()
	info := nativeSupportFileInfo(t, path)
	isSymlink := info.Mode()&os.ModeSymlink != 0
	if isSymlink != wantSymlink || !isSymlink && !info.Mode().IsRegular() {
		t.Fatalf("Gemini support artifact %s mode = %v, want symlink=%t", path, info.Mode(), wantSymlink)
	}
	target := nativeSupportResolvedTarget(t, path)
	targetInfo := nativeSupportFileInfo(t, target)
	data, err := os.ReadFile(path)
	if err != nil || !targetInfo.Mode().IsRegular() || targetInfo.Mode().Perm() != mode.Perm() || !bytes.Equal(data, contents) {
		t.Fatalf("Gemini support artifact %s = %q / %v / %v, want %q / regular %o", path, data, targetInfo.Mode(), err, contents, mode.Perm())
	}
	return info, target, targetInfo
}

func configureSharedGeminiHome(tb testing.TB) string {
	tb.Helper()
	home := tb.TempDir()
	tb.Setenv("HOME", home)
	tb.Setenv("XDG_STATE_HOME", tb.TempDir())
	tb.Setenv("XDG_CACHE_HOME", tb.TempDir())
	tb.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	return home
}

func TestGeminiSharedScopeDecodeErrorsIncludeContext(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		payload   string
		container bool
	}{
		{name: "fragment", payload: `{"mcpServers":`},
		{name: "servers", payload: `{"mcpServers":[]}`, container: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			home := configureSharedGeminiHome(t)
			key := outputKey{target: hcc.TargetGemini}
			const artifactPath = "gemini-extension.json"
			result := compilePhaseResult{
				allOutputs: map[outputKey]bool{key: true},
				byTarget: map[outputKey]*targetWork{
					key: {artifacts: []artifact.Artifact{{Path: artifactPath, Bytes: []byte(fixture.payload)}}},
				},
			}
			coalesced, err := coalesceGeminiNativeAggregates(&result, home, &Report{})
			if err == nil || coalesced.coalesced {
				t.Fatalf("invalid fragment was not refused: coalesced=%+v err=%v", coalesced, err)
			}
			if fixture.container {
				if _, ok := errors.AsType[*json.UnmarshalTypeError](err); !ok {
					t.Fatalf("JSON type error was not preserved: %v", err)
				}
				if !strings.Contains(err.Error(), "mcpServers") {
					t.Fatalf("decode error omits container context: %v", err)
				}
			} else if _, ok := errors.AsType[*json.SyntaxError](err); !ok {
				t.Fatalf("JSON syntax error was not preserved: %v", err)
			}
			if !strings.Contains(err.Error(), artifactPath) {
				t.Fatalf("decode error omits artifact context: %v", err)
			}
			report := executeMaterializePlans(t.Context(), result, false, home, config.MaterializationModes{})
			if !hasDiagCode(report.Diags, "materialize.destination.overlap") || len(report.Diags) != 1 || report.Diags[0].Message != err.Error() {
				t.Fatalf("decode error context did not reach the diagnostic: %+v", report.Diags)
			}
		})
	}
}

// spec: owned-entry-reconciliation/gemini-shared-settings-retain-both-scopes
func TestGeminiSharedScopeFileKeepsBothScopes(t *testing.T) {
	home := configureSharedGeminiHome(t)
	root := filepath.Join(home, ".gemini")
	file := filepath.Join(root, "settings.json")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  // handwritten setting\n  \"theme\": \"manual\",\n  \"mcpServers\": {\"handwritten\": {\"command\": \"manual-tool\"}}\n}\n")
	if err := os.WriteFile(file, original, 0o640); err != nil {
		t.Fatal(err)
	}
	desired := sharedGeminiResources(t)
	report, err := Run(context.Background(), home, desired)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("sync failed: %v %+v", err, report)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	settings := nativeMCPDocument(t, before)
	servers := settings["mcpServers"].(map[string]any)
	for _, name := range []string{"repository-mcp", "machine-mcp", "handwritten"} {
		if _, ok := servers[name]; !ok {
			t.Fatalf("shared settings lost %q: %s", name, before)
		}
	}
	for _, want := range []string{"// handwritten setting", "repository-audit", "machine-audit", "manual-tool"} {
		if !bytes.Contains(before, []byte(want)) {
			t.Fatalf("shared settings lost %q: %s", want, before)
		}
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("custom permissions changed: %v %v", info, err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof("settings.json")
	if err != nil {
		t.Fatalf("combined ownership proof: %v", err)
	}
	ownedIDs := proof.OwnedIDs()
	if len(ownedIDs) != 4 {
		t.Fatalf("combined ownership proof = %v, want both MCP servers and hooks", ownedIDs)
	}
	wantKinds := map[string]ownership.EntryKind{
		"repository-mcp":   ownership.KindMcpServer,
		"machine-mcp":      ownership.KindMcpServer,
		"repository-audit": ownership.KindHook,
		"machine-audit":    ownership.KindHook,
	}
	wantHookResources := map[string]bool{"repository-audit": true, "machine-audit": true}
	seenResources := make(map[string]bool, len(wantKinds))
	sourceDigests := make(map[string]string, len(wantHookResources))
	for _, id := range ownedIDs {
		entry, ok := proof.Entry(id)
		if !ok {
			t.Fatalf("owned identity %v has no entry", id)
		}
		wantKind, ok := wantKinds[entry.Resource]
		if !ok || id.Kind != wantKind || seenResources[entry.Resource] {
			t.Fatalf("owned entry = %+v, want one declared source and kind", entry)
		}
		seenResources[entry.Resource] = true
		if id.Kind == ownership.KindHook {
			if !wantHookResources[entry.Resource] || entry.SourceDigest == "" || len(entry.SourceEvidence) != 0 {
				t.Fatalf("owned hook source tuple = %+v, want one original resource with its scalar digest", entry)
			}
			sourceDigests[entry.Resource] = entry.SourceDigest
		} else if entry.SourceDigest != "" || len(entry.SourceEvidence) != 0 {
			t.Fatalf("MCP entry unexpectedly carries hook source evidence: %+v", entry)
		}
	}
	if len(seenResources) != len(wantKinds) || len(sourceDigests) != len(wantHookResources) || sourceDigests["repository-audit"] == sourceDigests["machine-audit"] {
		t.Fatalf("shared source identities = %+v / %+v, expected distinct repo and machine tuples", seenResources, sourceDigests)
	}
	for _, opts := range [][]RunOption{{WithDryRun()}, nil} {
		report, err := Run(context.Background(), home, desired, opts...)
		if err != nil || !report.InSync() {
			t.Fatalf("identical sync or check drifted: %v %+v", err, report)
		}
		after, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("identical sync changed bytes: %v", err)
		}
		afterInfo, err := os.Stat(file)
		if err != nil || !afterInfo.ModTime().Equal(info.ModTime()) {
			t.Fatalf("identical sync rewrote settings: %v", err)
		}
	}
	report, err = Run(context.Background(), home, desired[2:])
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("pruning failed: %v %+v", err, report)
	}
	after, err := os.ReadFile(file)
	if err != nil || bytes.Contains(after, []byte("repository-mcp")) || bytes.Contains(after, []byte("repository-audit")) || !bytes.Contains(after, []byte("machine-mcp")) || !bytes.Contains(after, []byte("handwritten")) {
		t.Fatalf("pruning changed the wrong scope: %v %s", err, after)
	}
}

// spec: owned-entry-reconciliation/gemini-shared-identity-conflict-refuses-mutation
// spec: owned-entry-reconciliation/gemini-shared-scope-restricted-mutation-refuses
func TestGeminiSharedScopeRefusalsPreserveSettingsAndOwnership(t *testing.T) {
	for _, operation := range []string{"conflict", "repository-only", "machine-only"} {
		t.Run(operation, func(t *testing.T) {
			home := configureSharedGeminiHome(t)
			desired := sharedGeminiResources(t)
			report, err := Run(context.Background(), home, desired)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("valid control failed: %v %+v", err, report)
			}
			root := filepath.Join(home, ".gemini")
			store, err := ownership.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			file := &RepairFile{OutDir: root, Rel: "settings.json", store: store}
			before := ownedCleanupDiskState(t, file)
			info, err := os.Stat(file.AbsFile())
			if err != nil {
				t.Fatal(err)
			}
			var opts []RunOption
			switch operation {
			case "conflict":
				name := "repository-mcp"
				desired[2].Entry = catalog.Entry{Kind: ir.KindMcpServer, Name: name, Resource: ir.NewMcpServer(name, "different-tool", nil, nil, nil, ir.SourceRef{})}
			case "repository-only":
				opts = []RunOption{WithRepoScopeOnly()}
			case "machine-only":
				opts = []RunOption{WithMachineScopeOnly()}
			}
			for _, dryRun := range []bool{false, true} {
				options := append([]RunOption(nil), opts...)
				if dryRun {
					options = append(options, WithDryRun())
				}
				report, err := Run(context.Background(), home, desired, options...)
				if err != nil || !hasDiagCode(report.Diags, "materialize.destination.overlap") {
					t.Fatalf("shared destination was not refused: %v %+v", err, report)
				}
				requireOwnedCleanupDiskState(t, file, before)
				after, err := os.Stat(file.AbsFile())
				if err != nil || !info.ModTime().Equal(after.ModTime()) || info.Mode() != after.Mode() {
					t.Fatalf("refusal changed file metadata: %v", err)
				}
			}
		})
	}
}

func TestGeminiSharedScopeCompatibleIdentityAndIncompleteInput(t *testing.T) {
	home := configureSharedGeminiHome(t)
	desired := sharedGeminiResources(t)
	desired[2].Entry = desired[0].Entry
	report, err := Run(context.Background(), home, desired)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("equal cross-scope identity refused: %v %+v", err, report)
	}
	desired[0].Resource = nil
	desired[2].Resource = nil
	report, err = Run(context.Background(), home, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagCode(report.Diags, "materialize.resource.nil") {
		t.Fatalf("incomplete input did not exercise the refusal fence: %v", report.Diags)
	}
	data, err := os.ReadFile(filepath.Join(home, ".gemini", "settings.json"))
	if err != nil || !bytes.Contains(data, []byte("repository-mcp")) {
		t.Fatalf("incomplete input pruned the existing owned entry: %v %s", err, data)
	}
}

// spec: owned-entry-reconciliation/shared-gemini-native-work-preserves-repository-commands
func TestGeminiSharedScopePreservesRepositoryCommands(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			for _, fixture := range []struct {
				name   string
				native bool
				alias  bool
			}{
				{name: "commands"},
				{name: "commands-and-native", native: true},
				{name: "alias-commands", alias: true},
				{name: "alias-commands-and-native", native: true, alias: true},
			} {
				t.Run(fixture.name, func(t *testing.T) {
					home := configureSharedGeminiHome(t)
					repo := home
					if fixture.alias {
						repo = filepath.Join(t.TempDir(), "home-alias")
						if err := os.Symlink(home, repo); err != nil {
							t.Fatal(err)
						}
					}
					var desired []resolver.ResolvedArtifact
					for _, name := range []string{"selected-command", "stale-command"} {
						command := ir.NewCommand(name, "Command fixture", "Inert fixture text", nil, ir.SourceRef{})
						desired = append(desired, resolver.ResolvedArtifact{
							Entry:  catalog.Entry{Kind: command.Kind(), Name: name, Resource: command},
							Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetGemini},
						})
					}
					if fixture.native {
						desired = append(desired, sharedGeminiResources(t)[:2]...)
					}
					modes := config.DefaultMaterializationModes()
					modes.Project, modes.Machine = mode, mode
					options := []RunOption{WithMaterializationModes(modes)}
					report, err := Run(t.Context(), repo, desired, options...)
					if err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("first sync failed: %v %+v", err, report)
					}
					commandPath := filepath.Join(home, ".gemini", "commands", "selected-command.toml")
					before, err := os.ReadFile(commandPath)
					if err != nil || !bytes.Contains(before, []byte("Inert fixture text")) {
						t.Fatalf("selected command missing after first sync: %v %s", err, before)
					}
					stalePath := filepath.Join(home, ".gemini", "commands", "stale-command.toml")
					if _, err := os.ReadFile(stalePath); err != nil {
						t.Fatalf("cleanup control did not install the stale command: %v", err)
					}
					info, err := os.Stat(commandPath)
					if err != nil {
						t.Fatal(err)
					}
					for _, dryRun := range []bool{false, true} {
						opts := append([]RunOption(nil), options...)
						if dryRun {
							opts = append(opts, WithDryRun())
						}
						report, err := Run(t.Context(), repo, desired, opts...)
						if err != nil || !report.InSync() {
							t.Fatalf("repeat sync or check drifted: %v %+v", err, report)
						}
						after, err := os.ReadFile(commandPath)
						if err != nil || !bytes.Equal(before, after) {
							t.Fatalf("repeat sync or check changed command bytes: %v", err)
						}
						afterInfo, err := os.Stat(commandPath)
						if err != nil || !afterInfo.ModTime().Equal(info.ModTime()) {
							t.Fatalf("repeat sync or check rewrote command: %v", err)
						}
					}
					desired = append(desired[:1:1], desired[2:]...)
					report, err = Run(t.Context(), repo, desired, options...)
					if err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("stale command cleanup failed: %v %+v", err, report)
					}
					if _, err := os.Stat(commandPath); err != nil {
						t.Fatalf("cleanup lost selected command: %v", err)
					}
					if _, err := os.Lstat(stalePath); !os.IsNotExist(err) {
						t.Fatalf("stale command was not pruned: %v", err)
					}
				})
			}
		})
	}
}

// spec: owned-entry-reconciliation/empty-shared-gemini-desired-set-retains-cleanup
func TestGeminiSharedScopeEmptyDesiredSetPrunesOutput(t *testing.T) {
	home := configureSharedGeminiHome(t)
	root := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(root, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{\"theme\":\"handwritten\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := ir.NewCommand("selected-command", "Command fixture", "Inert fixture text", nil, ir.SourceRef{})
	desired := append(sharedGeminiResources(t)[:2:2], resolver.ResolvedArtifact{
		Entry:  catalog.Entry{Kind: command.Kind(), Name: command.Name(), Resource: command},
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetGemini},
	})
	report, err := Run(t.Context(), home, desired)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("valid control failed: %v %+v", err, report)
	}
	commandPath := filepath.Join(root, "commands", "selected-command.toml")
	if _, err := os.ReadFile(commandPath); err != nil {
		t.Fatalf("valid control did not install the command: %v", err)
	}
	settings, err := os.ReadFile(settingsPath)
	if err != nil || !bytes.Contains(settings, []byte("repository-mcp")) || !bytes.Contains(settings, []byte("repository-audit")) {
		t.Fatalf("valid control did not install native entries: %v %s", err, settings)
	}
	report, err = Run(t.Context(), home, nil)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("cleanup failed: %v %+v", err, report)
	}
	if _, err := os.Lstat(commandPath); !os.IsNotExist(err) {
		t.Fatalf("empty desired set did not prune stale command: %v", err)
	}
	settings, err = os.ReadFile(settingsPath)
	if err != nil || !bytes.Contains(settings, []byte("handwritten")) || bytes.Contains(settings, []byte("repository-mcp")) || bytes.Contains(settings, []byte("repository-audit")) {
		t.Fatalf("cleanup changed the wrong settings: %v %s", err, settings)
	}
}

func TestGeminiSharedScopeSupportPublicationPreservesOriginalPolicies(t *testing.T) {
	loc := nativeSupportLocationFor(t, hcc.TargetGemini)
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			home := configureSharedGeminiHome(t)
			stateRoot := filepath.Join(home, "state")
			cacheRoot := filepath.Join(home, "cache")
			t.Setenv("XDG_STATE_HOME", stateRoot)
			t.Setenv("XDG_CACHE_HOME", cacheRoot)

			repositoryScript := []byte(nativeSupportScriptBody)
			machineScript := []byte(nativeSupportScriptBody)
			repositoryPayload := []byte(`{"owner":"repository-support"}`)
			machinePayload := []byte(`{"owner":"machine-support"}`)
			repositoryHook, _ := geminiSupportHook(t, "repository-support", config.RepoCommitted, repositoryScript, repositoryPayload)
			machineHook, _ := geminiSupportHook(t, "machine-support", config.UserPersonal, machineScript, machinePayload)
			repositoryMCP := sharedGeminiMCPResource("repository", config.RepoCommitted)
			machineMCP := sharedGeminiMCPResource("machine", config.UserPersonal)
			desired := []resolver.ResolvedArtifact{repositoryMCP, repositoryHook, machineMCP, machineHook}
			modes := config.MaterializationModes{Project: mode, Machine: mode}
			modeOption := WithMaterializationModes(modes)

			report, err := Run(context.Background(), home, desired, modeOption)
			if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("shared support sync failed: %v %+v", err, report)
			}
			nativeRoot := filepath.Join(home, ".gemini")
			settingsPath := filepath.Join(nativeRoot, "settings.json")
			commands := nativeSupportCommands(t, loc, settingsPath, 2)
			var repositoryCommand, machineCommand string
			for _, command := range commands {
				switch {
				case strings.Contains(command, "repository-support"):
					repositoryCommand = command
				case strings.Contains(command, "machine-support"):
					machineCommand = command
				}
			}
			if repositoryCommand == "" || machineCommand == "" {
				t.Fatalf("shared Gemini support commands = %v, want both original scope resources", commands)
			}
			repositoryScriptPath, _ := loc.installedRef(t, repositoryCommand, home, home, false)
			machineScriptPath, _ := loc.installedRef(t, machineCommand, home, home, true)
			repositoryPayloadPath := filepath.Join(filepath.Dir(repositoryScriptPath), "payload.json")
			machinePayloadPath := filepath.Join(filepath.Dir(machineScriptPath), "payload.json")
			machineLink := mode == config.MaterializationModeSymlink
			repositoryScriptInfo, repositoryScriptTarget, repositoryScriptTargetInfo := assertGeminiSupportArtifact(t, repositoryScriptPath, false, repositoryScript, 0o755)
			repositoryPayloadInfo, repositoryPayloadTarget, repositoryPayloadTargetInfo := assertGeminiSupportArtifact(t, repositoryPayloadPath, false, repositoryPayload, 0o644)
			machineScriptInfo, machineScriptTarget, machineScriptTargetInfo := assertGeminiSupportArtifact(t, machineScriptPath, machineLink, machineScript, 0o755)
			machinePayloadInfo, machinePayloadTarget, machinePayloadTargetInfo := assertGeminiSupportArtifact(t, machinePayloadPath, machineLink, machinePayload, 0o644)

			store, err := ownership.Open(nativeRoot)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := store.Proof("settings.json")
			if err != nil {
				t.Fatalf("shared ownership proof: %v", err)
			}
			wantKinds := map[string]ownership.EntryKind{
				"repository-mcp":     ownership.KindMcpServer,
				"machine-mcp":        ownership.KindMcpServer,
				"repository-support": ownership.KindHook,
				"machine-support":    ownership.KindHook,
			}
			seen := make(map[string]bool, len(wantKinds))
			sourceDigests := make(map[string]string, 2)
			var machineHookID ownership.EntryID
			var machineHookEntry ownership.Entry
			for _, id := range proof.OwnedIDs() {
				entry, ok := proof.Entry(id)
				if !ok {
					t.Fatalf("owned identity %v has no entry", id)
				}
				kind, ok := wantKinds[entry.Resource]
				if !ok || id.Kind != kind || seen[entry.Resource] {
					t.Fatalf("shared support ownership entry = %+v", entry)
				}
				seen[entry.Resource] = true
				if id.Kind == ownership.KindHook {
					if entry.SourceDigest == "" || len(entry.SourceEvidence) != 0 {
						t.Fatalf("shared hook source tuple = %+v, want an original scalar source digest", entry)
					}
					sourceDigests[entry.Resource] = entry.SourceDigest
					if entry.Resource == "machine-support" {
						machineHookID = id
						machineHookEntry = entry
					}
				} else if entry.SourceDigest != "" || len(entry.SourceEvidence) != 0 {
					t.Fatalf("MCP entry unexpectedly carries hook source evidence: %+v", entry)
				}
			}
			if len(seen) != len(wantKinds) || len(sourceDigests) != 2 || sourceDigests["repository-support"] == sourceDigests["machine-support"] || machineHookID == (ownership.EntryID{}) {
				t.Fatalf("shared hook source identities = %+v / %+v", seen, sourceDigests)
			}

			repositoryManifest, err := portableManifestPath(home, false)
			if err != nil {
				t.Fatal(err)
			}
			machineManifest, err := portableManifestPath(home, true)
			if err != nil {
				t.Fatal(err)
			}
			identityPaths := map[string]os.FileInfo{
				settingsPath:            nativeSupportFileInfo(t, settingsPath),
				store.StateFile():       nativeSupportFileInfo(t, store.StateFile()),
				repositoryManifest:      nativeSupportFileInfo(t, repositoryManifest),
				repositoryScriptPath:    repositoryScriptInfo,
				repositoryPayloadPath:   repositoryPayloadInfo,
				machineScriptPath:       machineScriptInfo,
				machinePayloadPath:      machinePayloadInfo,
				repositoryScriptTarget:  repositoryScriptTargetInfo,
				repositoryPayloadTarget: repositoryPayloadTargetInfo,
				machineScriptTarget:     machineScriptTargetInfo,
				machinePayloadTarget:    machinePayloadTargetInfo,
			}
			if mode == config.MaterializationModeFile {
				identityPaths[machineManifest] = nativeSupportFileInfo(t, machineManifest)
			} else {
				assertPathAbsent(t, machineManifest)
			}
			identitySnapshot := func() map[string]nativeSupportState {
				t.Helper()
				return nativeSupportPersistentSnapshotWithStableArtifactDirectoryTimes(t, home, home)
			}
			assertMachineManifestAbsent := func() {
				if mode == config.MaterializationModeSymlink {
					assertPathAbsent(t, machineManifest)
				}
			}
			assertUnchanged := func(before map[string]nativeSupportState, beforeFiles map[string]os.FileInfo) {
				t.Helper()
				assertNativeSupportSnapshot(t, before, identitySnapshot())
				for path, info := range beforeFiles {
					assertNativeSupportSameFile(t, path, info)
				}
			}
			stableBefore := identitySnapshot()
			for _, opts := range [][]RunOption{{WithDryRun()}, nil} {
				options := append([]RunOption{modeOption}, opts...)
				report, err := Run(context.Background(), home, desired, options...)
				if err != nil || report == nil || !report.InSync() {
					t.Fatalf("repeat or check drifted: %v %+v", err, report)
				}
				assertUnchanged(stableBefore, identityPaths)
				assertMachineManifestAbsent()
			}

			conflict := append([]resolver.ResolvedArtifact(nil), desired...)
			conflictName := "repository-mcp"
			conflict[2].Entry = catalog.Entry{Kind: ir.KindMcpServer, Name: conflictName, Resource: ir.NewMcpServer(conflictName, "different-tool", nil, nil, nil, ir.SourceRef{})}
			refusals := []struct {
				name      string
				resources []resolver.ResolvedArtifact
				scope     RunOption
			}{
				{name: "conflicting MCP", resources: conflict},
				{name: "repository-only", resources: desired, scope: WithRepoScopeOnly()},
				{name: "machine-only", resources: desired, scope: WithMachineScopeOnly()},
			}
			for _, refusal := range refusals {
				for _, dryRun := range []bool{false, true} {
					before := identitySnapshot()
					beforeFiles := make(map[string]os.FileInfo, len(identityPaths))
					for path, info := range identityPaths {
						beforeFiles[path] = info
					}
					options := []RunOption{modeOption}
					if refusal.scope != nil {
						options = append(options, refusal.scope)
					}
					if dryRun {
						options = append(options, WithDryRun())
					}
					report, err := Run(context.Background(), home, refusal.resources, options...)
					if err != nil || report == nil || !hasDiagCode(report.Diags, "materialize.destination.overlap") {
						t.Fatalf("%s dry=%t was not refused: %v %+v", refusal.name, dryRun, err, report)
					}
					assertUnchanged(before, beforeFiles)
					assertMachineManifestAbsent()
				}
			}

			machineOnly := []resolver.ResolvedArtifact{machineMCP, machineHook}
			report, err = Run(context.Background(), home, machineOnly, modeOption)
			if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("repository deselection failed: %v %+v", err, report)
			}
			afterSettings, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatal(err)
			}
			servers := nativeMCPDocument(t, afterSettings)["mcpServers"].(map[string]any)
			if _, ok := servers["repository-mcp"]; ok || servers["machine-mcp"] == nil {
				t.Fatalf("repository deselection changed the wrong MCP scope: %s", afterSettings)
			}
			remainingCommands := nativeSupportCommands(t, loc, settingsPath, 1)
			if remainingCommands[0] != machineCommand {
				t.Fatalf("repository deselection changed the machine command: %q != %q", remainingCommands[0], machineCommand)
			}
			assertGeminiSupportArtifact(t, machineScriptPath, machineLink, machineScript, 0o755)
			assertGeminiSupportArtifact(t, machinePayloadPath, machineLink, machinePayload, 0o644)
			assertNativeSupportSameFile(t, machineScriptPath, machineScriptInfo)
			assertNativeSupportSameFile(t, machinePayloadPath, machinePayloadInfo)
			assertNativeSupportSameFile(t, machineScriptTarget, machineScriptTargetInfo)
			assertNativeSupportSameFile(t, machinePayloadTarget, machinePayloadTargetInfo)
			afterProof, err := store.Proof("settings.json")
			if err != nil {
				t.Fatalf("ownership proof after repository deselection: %v", err)
			}
			if len(afterProof.OwnedIDs()) != 2 {
				t.Fatalf("ownership after repository deselection = %v, want machine MCP and hook", afterProof.OwnedIDs())
			}
			retainedMachineHook := false
			for _, id := range afterProof.OwnedIDs() {
				entry, ok := afterProof.Entry(id)
				if !ok {
					t.Fatalf("owned identity %v has no entry after deselection", id)
				}
				if entry.Resource == "machine-support" {
					retainedMachineHook = id == machineHookID && entry.SourceDigest == machineHookEntry.SourceDigest && entry.ValueDigest == machineHookEntry.ValueDigest && len(entry.SourceEvidence) == 0
				}
				if entry.Resource == "repository-mcp" || entry.Resource == "repository-support" {
					t.Fatalf("repository ownership survived deselection: %+v", entry)
				}
			}
			if !retainedMachineHook {
				t.Fatal("machine hook source tuple did not survive repository deselection")
			}
			assertMachineManifestAbsent()
		})
	}
}

func BenchmarkSharedGeminiNativeSyncDisk(b *testing.B) {
	discardBenchLogs(b)
	home := configureSharedGeminiHome(b)
	desired := sharedGeminiResources(b)
	if _, err := Run(context.Background(), home, desired); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Run(context.Background(), home, desired); err != nil {
			b.Fatal(err)
		}
	}
}
