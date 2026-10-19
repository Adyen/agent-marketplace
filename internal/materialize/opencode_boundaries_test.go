package materialize

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: opencode-hooks/adapter-recognition-does-not-authorize-overwrite
func TestOpenCodeUnmanagedAdapterPreservesHookGeneration(t *testing.T) {
	repo := openCodeSandbox(t)
	h := ir.NewHookWithMatcher("check", "PreToolUse", "Bash", "true", "command", nil, ir.SourceRef{})
	compiled, err := hcc.Compile(h, hcc.TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, ".opencode", "plugins", "agent-marketplace.js")
	additiveSafetyWrite(t, file, string(compiled.Artifacts[1].Bytes))
	before, _ := os.ReadFile(file)
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{openCodeResource(h), openCodeResource(ir.NewCommand("control", "", "Control", nil, ir.SourceRef{}))})
	if err != nil || !diag.HasErrors(report.Diags) {
		t.Fatalf("unmanaged adapter accepted: %v %v", err, report.Diags)
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) {
		t.Fatal("recognized unmanaged adapter changed")
	}
	if _, err := os.Lstat(filepath.Join(repo, ".opencode", "hooks", "agent-marketplace", "check.json")); !os.IsNotExist(err) {
		t.Fatal("declaration published without owned adapter")
	}
	if _, err := os.Stat(filepath.Join(repo, ".opencode", "commands", "control.md")); err != nil {
		t.Fatal("independent command blocked")
	}
}

// spec: opencode-native-configuration/effective-native-identity-collisions-do-not-shadow-handwritten-agents
func TestOpenCodeAgentIdentityCollisions(t *testing.T) {
	repo := openCodeSandbox(t)
	additiveSafetyWrite(t, filepath.Join(repo, "opencode.jsonc"), `{"agent":{"manual":{"mode":"primary"}}}`)
	additiveSafetyWrite(t, filepath.Join(repo, ".opencode", "opencode.json"), `{ "theme":"system" }`)
	var desired []resolver.ResolvedArtifact
	for _, name := range []string{"manual", "build", "reviewer"} {
		desired = append(desired, openCodeResource(ir.NewSubagent(name, "Review", "Body", "", nil, "", nil, "", nil, ir.SourceRef{})))
	}
	report, err := Run(t.Context(), repo, desired)
	if err != nil || !diag.HasErrors(report.Diags) {
		t.Fatal("colliding agent identities accepted")
	}
	for _, name := range []string{"manual", "build"} {
		if _, err := os.Lstat(filepath.Join(repo, ".opencode", "agents", name+".md")); !os.IsNotExist(err) {
			t.Fatal("colliding agent published")
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".opencode", "agents", "reviewer.md")); err != nil {
		t.Fatal("valid agent blocked")
	}
}

// spec: opencode-native-configuration/malformed-shared-configuration-refuses-affected-mutation
func TestOpenCodeMalformedAggregatePreservesBytes(t *testing.T) {
	for _, data := range []string{`{"mcp":{},"mcp":{}}`, `{"mcp":[]}`, `{"mcp":`} {
		t.Run(data, func(t *testing.T) {
			repo := openCodeSandbox(t)
			file := filepath.Join(repo, "opencode.jsonc")
			additiveSafetyWrite(t, file, data)
			report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{openCodeResource(ir.NewMcpServer("docs", "server", nil, nil, nil, ir.SourceRef{})), openCodeResource(ir.NewSkill("control", "", "Body", nil, nil, ir.SourceRef{}))})
			if err != nil || !diag.HasErrors(report.Diags) {
				t.Fatal("malformed aggregate accepted")
			}
			after, _ := os.ReadFile(file)
			if string(after) != data {
				t.Fatal("malformed aggregate changed")
			}
			if _, err := os.Stat(filepath.Join(repo, ".opencode", "skills", "control", "SKILL.md")); err != nil {
				t.Fatal("independent resource blocked")
			}
		})
	}
}

// spec: opencode-native-configuration/ambiguous-overlapping-scope-operations-refuse
func TestOpenCodeSharedOverrideRefusesBeforeWriting(t *testing.T) {
	repo := openCodeSandbox(t)
	custom := filepath.Join(os.Getenv("HOME"), "shared")
	t.Setenv("OPENCODE_CONFIG_DIR", custom)
	project := openCodeResource(ir.NewCommand("project", "", "Project", nil, ir.SourceRef{}))
	machine := openCodeResource(ir.NewCommand("machine", "", "Machine", nil, ir.SourceRef{}))
	machine.Source = config.UserPersonal
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{project, machine})
	if err != nil || !diag.HasCode(report.Diags, diag.CodeMaterializeDestinationOverlap) {
		t.Fatal("shared override accepted")
	}
	if _, err := os.Lstat(custom); !os.IsNotExist(err) {
		t.Fatal("overlap refusal created native output")
	}
	for _, scoped := range []RunOption{WithRepoScopeOnly(), WithMachineScopeOnly()} {
		report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{project, machine}, scoped)
		if err != nil || !diag.HasCode(report.Diags, diag.CodeMaterializeDestinationOverlap) {
			t.Fatal("scope filter concealed shared override")
		}
	}
}

func TestOpenCodeSingleScopeCustomRootRepeats(t *testing.T) {
	repo := openCodeSandbox(t)
	custom := filepath.Join(os.Getenv("HOME"), "custom")
	t.Setenv("OPENCODE_CONFIG_DIR", custom)
	resources := []resolver.ResolvedArtifact{
		openCodeResource(ir.NewCommand("check", "Check", "Body", nil, ir.SourceRef{})),
		openCodeResource(ir.NewMcpServer("server", "server", nil, nil, nil, ir.SourceRef{})),
	}
	for range 2 {
		report, err := Run(t.Context(), repo, resources)
		if err != nil || diag.HasErrors(report.Diags) {
			t.Fatalf("single-scope override: %v %v", err, report.Diags)
		}
	}
	if _, err := os.Stat(filepath.Join(custom, "commands", "check.md")); err != nil {
		t.Fatal(err)
	}
	if report, err := Run(t.Context(), repo, nil, WithRepoScopeOnly()); err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("custom cleanup: %v %v", err, report.Diags)
	}
	if _, err := os.Lstat(filepath.Join(custom, "commands", "check.md")); !os.IsNotExist(err) {
		t.Fatal("custom output survived removal")
	}
}

// spec: opencode-native-configuration/unsafe-destination-or-payload-preserves-external-state
func TestOpenCodeEscapingRootRefused(t *testing.T) {
	repo := openCodeSandbox(t)
	outside := t.TempDir()
	additiveSafetyWrite(t, filepath.Join(outside, "sentinel"), "unchanged")
	if err := os.Symlink(outside, filepath.Join(repo, ".opencode")); err != nil {
		t.Fatal(err)
	}
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{openCodeResource(ir.NewCommand("check", "", "Body", nil, ir.SourceRef{}))})
	if err != nil || !diag.HasErrors(report.Diags) {
		t.Fatal("escaping root accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 || entries[0].Name() != "sentinel" {
		t.Fatal("foreign destination changed")
	}
}

// spec: opencode-hooks/support-refusal-retains-the-installed-hook-generation
func TestOpenCodeHookSupportRefusalRetainsInstallation(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(fmt.Sprintf("machine=%t", machine), func(t *testing.T) {
			repo := openCodeSandbox(t)
			selected := additiveSafetyProvider(t, hcc.TargetOpenCode, machine, `/bin/sh "${DROID_PLUGIN_ROOT}/shared.sh"`, "exit 0\n")
			if report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{selected}); err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("seed: %v %v", err, report.Diags)
			}
			root := openCodeSupportRoot(t, repo, machine)
			snapshot := func() map[string]string {
				files := map[string]string{}
				_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
					if err == nil && !entry.IsDir() {
						data, _ := os.ReadFile(path)
						rel, _ := filepath.Rel(root, path)
						files[filepath.ToSlash(rel)] = string(data)
					}
					return err
				})
				return files
			}
			before := snapshot()
			if len(openCodeSupportFiles(t, root)) != 1 {
				t.Fatalf("seeded support = %v", before)
			}
			// Replace the support script with a link outside the source.
			script := filepath.Join(filepath.Dir(selected.Path), "shared.sh")
			outside := filepath.Join(t.TempDir(), "outside.sh")
			additiveSafetyWrite(t, outside, "exit 1\n")
			if err := os.Remove(script); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, script); err != nil {
				t.Fatal(err)
			}
			report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{selected})
			if err != nil || !diag.HasCode(report.Diags, diag.CodeMaterializeHookSupportRefused) {
				t.Fatalf("unsafe support accepted: %v %v", err, report.Diags)
			}
			if after := snapshot(); !maps.Equal(before, after) {
				t.Fatalf("refused support changed the installed generation:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

// spec: opencode-hooks/sibling-declarations-are-never-installed
func TestOpenCodeHookSiblingDeclarationsAreNeverInstalled(t *testing.T) {
	compiled, err := hcc.Compile(ir.NewHookWithMatcher("seed", "PreToolUse", "Bash", "true", "command", nil, ir.SourceRef{}), hcc.TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	var adapter []byte
	for _, a := range compiled.Artifacts {
		if a.Path == "plugins/agent-marketplace.js" {
			adapter = a.Bytes
		}
	}
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			repo, registry := openCodeSandbox(t), t.TempDir()
			declaration := func(name, command string) string {
				return `{"format":"agent-marketplace/opencode-hook-v1","name":"` + name + `","event":"PreToolUse","matcher":"Bash","command":"` + command + `","timeout":30}`
			}
			for name, content := range map[string]string{
				".opencode/plugins/agent-marketplace.js":          string(adapter),
				".opencode/hooks/agent-marketplace/selected.json": declaration("selected", "true"),
				".opencode/hooks/agent-marketplace/other.json":    declaration("other", "false"),
				".opencode/hooks/agent-marketplace/check.sh":      "true\n",
			} {
				additiveSafetyWrite(t, filepath.Join(registry, filepath.FromSlash(name)), content)
			}
			cat, err := catalog.Load(registry, "opencode")
			if err != nil {
				t.Fatal(err)
			}
			var selected []resolver.ResolvedArtifact
			for _, entry := range cat.Entries {
				if entry.Name == "selected" {
					selected = append(selected, resolver.ResolvedArtifact{Entry: entry, RegistryURL: registry, CatalogKey: registry, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetOpenCode}})
				}
			}
			if len(selected) != 1 {
				t.Fatalf("catalog entries = %v", cat.Entries)
			}
			if report, err := Run(t.Context(), repo, selected, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})); err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("sync: %v %v", err, report.Diags)
			}
			var installed []string
			root := filepath.Join(repo, ".opencode")
			if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err == nil && !entry.IsDir() {
					rel, _ := filepath.Rel(root, path)
					installed = append(installed, filepath.ToSlash(rel))
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			slices.Sort(installed)
			if len(installed) != 3 || installed[0] != "hook-support/"+strings.Split(installed[0], "/")[1]+"/selected/agent-marketplace/check.sh" || installed[1] != "hooks/agent-marketplace/selected.json" || installed[2] != "plugins/agent-marketplace.js" {
				t.Fatalf("installed %v, want the selected declaration, its support script, and the adapter", installed)
			}
		})
	}
}

// spec: opencode-native-configuration/concurrent-or-interrupted-updates-preserve-an-authorized-generation
func TestOpenCodeInterruptedAggregateRecoversOnSync(t *testing.T) {
	repo := openCodeSandbox(t)
	resource := func(command string) []resolver.ResolvedArtifact {
		return []resolver.ResolvedArtifact{openCodeResource(ir.NewMcpServer("managed", command, nil, nil, nil, ir.SourceRef{}))}
	}
	if _, err := Run(t.Context(), repo, resource("first")); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(repo, ownership.WithCrasher(ownership.CrashAt(ownership.CrashAfterAggregateRename)))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "opencode.json")
	before, _ := os.ReadFile(file)
	next := bytes.ReplaceAll(before, []byte("first"), []byte("second"))
	value := []byte(`{"command":["second"],"type":"local"}`)
	_, err = store.Apply(ownership.Mutation{Rel: "opencode.json", AbsFile: file, NextBytes: next, Entries: []ownership.Entry{{Kind: ownership.KindMcpServer, Container: "mcp", Key: "managed", IdentityDigest: ownership.McpIdentityDigest("managed"), ValueDigest: ownership.Digest(value), Resource: "managed"}}, Reason: ownership.ReasonSync, ExpectPriorDigest: ownership.Digest(before), ExpectPriorGeneration: record.Generation})
	if !errors.Is(err, ownership.ErrInjectedCrash) {
		t.Fatalf("crash boundary not reached: %v", err)
	}
	report, err := Run(context.Background(), repo, resource("second"))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("recover: %v %v", err, report.Diags)
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(after, next) {
		t.Fatal("recovery changed intended generation")
	}
}

func TestOpenCodeRepairRefusesSharedOverride(t *testing.T) {
	repo := openCodeSandbox(t)
	custom := filepath.Join(os.Getenv("HOME"), "shared.json")
	t.Setenv("OPENCODE_CONFIG", custom)
	initial := `{"mcp":{"project":{"type":"local","command":["server"]},"machine":{"type":"local","command":["server"]}}}`
	additiveSafetyWrite(t, custom, initial)
	project := openCodeResource(ir.NewMcpServer("project", "server", nil, nil, nil, ir.SourceRef{}))
	machine := openCodeResource(ir.NewMcpServer("machine", "server", nil, nil, nil, ir.SourceRef{}))
	machine.Source = config.UserPersonal
	preview, err := OpenCodeRepairPreview(t.Context(), repo, []resolver.ResolvedArtifact{project, machine})
	if err != nil || !diag.HasCode(preview.Diags, diag.CodeMaterializeDestinationOverlap) || len(preview.Files) != 0 {
		t.Fatalf("shared repair: %v %#v", err, preview)
	}
	after, _ := os.ReadFile(custom)
	if string(after) != initial {
		t.Fatal("preview changed shared configuration")
	}
	preview, err = OpenCodeRepairPreview(t.Context(), repo, []resolver.ResolvedArtifact{project})
	if err != nil || diag.HasErrors(preview.Diags) {
		t.Fatalf("single-scope repair: %v %#v", err, preview)
	}
}

func TestOpenCodeRepairReportsCompileFailure(t *testing.T) {
	repo := openCodeSandbox(t)
	additiveSafetyWrite(t, filepath.Join(repo, "opencode.json"), `{"mcp":{}}`)
	server := ir.NewMcpServer("server", "server", nil, nil, map[string]any{"unsupported": true}, ir.SourceRef{})
	preview, err := OpenCodeRepairPreview(t.Context(), repo, []resolver.ResolvedArtifact{openCodeResource(server)})
	if err != nil || !diag.HasCode(preview.Diags, diag.CodeMcpOpenCodeUnrepresentable) {
		t.Fatalf("repair compile failure: %v %#v", err, preview)
	}
}

func TestOpenCodeFileOverrideScopeTransitionPrunesResources(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeFile, config.MaterializationModeSymlink} {
		for _, firstMachine := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/machine-first-%v", mode, firstMachine), func(t *testing.T) {
				repo := openCodeSandbox(t)
				file := filepath.Join(os.Getenv("HOME"), "shared.jsonc")
				t.Setenv("OPENCODE_CONFIG", file)
				additiveSafetyWrite(t, file, "{ // handwritten\n}")
				resources := func(machine bool) []resolver.ResolvedArtifact {
					var selected []resolver.ResolvedArtifact
					for _, r := range []ir.Resource{ir.NewCommand("check", "", "Body", nil, ir.SourceRef{}), ir.NewMcpServer("server", "server", nil, nil, nil, ir.SourceRef{})} {
						x := openCodeResource(r)
						if machine {
							x.Source = config.UserPersonal
						}
						selected = append(selected, x)
					}
					return selected
				}
				options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				for _, scope := range []bool{firstMachine, !firstMachine, !firstMachine} {
					report, err := Run(t.Context(), repo, resources(scope), options...)
					if err != nil || diag.HasErrors(report.Diags) {
						t.Fatalf("scope transition: %v %v", err, report.Diags)
					}
				}
				project := filepath.Join(repo, ".opencode", "commands", "check.md")
				machine := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "commands", "check.md")
				old, current := project, machine
				if firstMachine {
					old, current = machine, project
				}
				if _, err := os.Lstat(old); !os.IsNotExist(err) {
					t.Fatal("removed scope resource remains installed")
				}
				if _, err := os.Stat(current); err != nil {
					t.Fatal("selected scope resource missing")
				}
				data, _ := os.ReadFile(file)
				if !bytes.Contains(data, []byte(`"server"`)) || !bytes.Contains(data, []byte("// handwritten")) {
					t.Fatal("shared aggregate lost desired entry or comment")
				}
				if report, err := Run(t.Context(), repo, nil, options...); err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("empty cleanup: %v %v", err, report.Diags)
				}
				data, _ = os.ReadFile(file)
				if bytes.Contains(data, []byte(`"server"`)) {
					t.Fatal("shared aggregate survived final removal")
				}
			})
		}
	}
}

func TestOpenCodeFileOverrideEmptySyncPrunesSingleScope(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(fmt.Sprint(machine), func(t *testing.T) {
			repo := openCodeSandbox(t)
			file := filepath.Join(os.Getenv("HOME"), "shared.json")
			t.Setenv("OPENCODE_CONFIG", file)
			resources := []resolver.ResolvedArtifact{
				openCodeResource(ir.NewCommand("check", "", "Body", nil, ir.SourceRef{})),
				openCodeResource(ir.NewMcpServer("server", "server", nil, nil, nil, ir.SourceRef{})),
			}
			if machine {
				for i := range resources {
					resources[i].Source = config.UserPersonal
				}
			}
			if report, err := Run(t.Context(), repo, resources); err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("seed: %v %v", err, report.Diags)
			}
			if report, err := Run(t.Context(), repo, nil); err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("cleanup: %v %v", err, report.Diags)
			}
			data, err := os.ReadFile(file)
			if err != nil || bytes.Contains(data, []byte(`"server"`)) {
				t.Fatal("removed single-scope MCP remains installed")
			}
		})
	}
}

// spec: opencode-native-configuration/unsafe-destination-or-payload-preserves-external-state
func TestOpenCodeConfigFileOverrideKeepsRepositoryBoundary(t *testing.T) {
	repo := openCodeSandbox(t)
	configDir, outside := t.TempDir(), t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(configDir, "opencode.json"))
	key := outputKey{target: hcc.TargetOpenCode}
	destinations, err := harness.ResolveDestinations(key.target, false, repo)
	if err != nil {
		t.Fatal(err)
	}
	// The resource root is swapped for a link after destinations resolved.
	if err := os.Symlink(outside, filepath.Join(repo, ".opencode")); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	boundary := destinationBoundary{machineScope: destinationBoundaryMachineScope(key), repoRoot: repo, homeDir: home}
	configureDestinationBoundary(&boundary, key, destinations[0].Root, repo, destinations)
	if err := boundary.directory(filepath.Join(repo, ".opencode", "commands"), true); err == nil {
		t.Fatal("a linked repository .opencode was followed under OPENCODE_CONFIG")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("output escaped the repository: %v", entries)
	}
	// The relocated configuration file stays writable through its own root.
	if err := boundary.directory(configDir, true); err != nil {
		t.Fatalf("external configuration root refused: %v", err)
	}
}
