package materialize

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestNativeHookDefinitionsDoNotConflict(t *testing.T) {
	tests := []struct {
		name          string
		sourceHarness string
		write         func(t *testing.T, root, command string, index int)
		selectHook    func(ir.Resource) bool
		excluded      []string
		wantPayload   bool
	}{
		{
			name:          "devin project hooks",
			sourceHarness: "devin",
			write: func(t *testing.T, root, command string, index int) {
				dir := filepath.Join(root, ".devin")
				additiveSafetyWrite(t, filepath.Join(dir, "hooks.v1.json"), fmt.Sprintf(`{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":%q}]}]}`, command))
				addHookSupportFiles(t, dir, index)
			},
			excluded:    []string{"hooks.v1.json", "config.json", "mcp_config.json"},
			wantPayload: true,
		},
		{
			name:          "devin user config",
			sourceHarness: "devin",
			write: func(t *testing.T, root, command string, index int) {
				dir := filepath.Join(root, ".devin")
				additiveSafetyWrite(t, filepath.Join(dir, "config.json"), fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":%q}]}]}}`, command))
				addHookSupportFiles(t, dir, index)
			},
			excluded:    []string{"hooks.v1.json", "config.json", "mcp_config.json"},
			wantPayload: true,
		},
		{
			name:          "windsurf native hooks",
			sourceHarness: "windsurf",
			write: func(t *testing.T, root, command string, index int) {
				dir := filepath.Join(root, ".windsurf")
				additiveSafetyWrite(t, filepath.Join(dir, "hooks.json"), fmt.Sprintf(`{"hooks":{"pre_run_command":[{"command":%q}]}}`, command))
				additiveSafetyWrite(t, filepath.Join(dir, "mcp_config.json"), fmt.Sprintf(`{"mcpServers":{"server-%d":{"command":"echo"}}}`, index))
				addHookSupportFiles(t, dir, index)
			},
			excluded:    []string{"hooks.json", "mcp_config.json"},
			wantPayload: true,
		},
		{
			name:          "gemini native settings",
			sourceHarness: "gemini",
			write: func(t *testing.T, root, command string, index int) {
				dir := filepath.Join(root, ".gemini")
				// run_shell_command is Gemini's exact shell tool name; the
				// catalog resolver maps it to Windsurf's pre_run_command.
				additiveSafetyWrite(t, filepath.Join(dir, "settings.json"), fmt.Sprintf(`{"hooks":{"BeforeTool":[{"matcher":"run_shell_command","hooks":[{"type":"command","command":%q}]}]}}`, command))
				addHookSupportFiles(t, dir, index)
			},
			excluded:    []string{"settings.json"},
			wantPayload: true,
		},
		{
			name:          "codex native hooks",
			sourceHarness: "codex",
			write: func(t *testing.T, root, command string, index int) {
				dir := filepath.Join(root, ".codex")
				additiveSafetyWrite(t, filepath.Join(dir, "hooks.json"), fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":%q}]}]}}`, command))
				additiveSafetyWrite(t, filepath.Join(dir, "config.toml"), fmt.Sprintf("[mcp_servers.server-%d]\ncommand = 'echo'\n", index))
				addHookSupportFiles(t, dir, index)
			},
			excluded:    []string{"hooks.json", "config.toml"},
			wantPayload: true,
		},
		{
			name:          "windsurf legacy hooks",
			sourceHarness: "windsurf",
			write: func(t *testing.T, root, command string, index int) {
				dir := filepath.Join(root, ".windsurf", "hooks")
				additiveSafetyWrite(t, filepath.Join(dir, "pre_run_command.json"), fmt.Sprintf(`{"event":"pre_run_command","type":"command","command":%q}`, command))
				additiveSafetyWrite(t, filepath.Join(dir, "other.json"), fmt.Sprintf(`{"event":"post_run_command","type":"command","command":"echo other-%d"}`, index))
				addHookSupportFiles(t, dir, index)
			},
			selectHook: func(resource ir.Resource) bool {
				hook, ok := resource.(*ir.Hook)
				return ok && hook.Name() == "pre_run_command"
			},
			excluded: []string{"pre_run_command.json", "other.json", "payload.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
				t.Run(string(mode), func(t *testing.T) {
					repo, _ := additiveSafetyEnv(t)
					cfg := &config.Config{}
					catalogs := map[string][]catalog.Entry{}
					for i, command := range []string{"echo first", "echo second"} {
						root := t.TempDir()
						tt.write(t, root, command, i)
						var cat *catalog.Catalog
						var err error
						if tt.name == "windsurf legacy hooks" {
							cat, err = catalog.LoadWithOptions(root, tt.sourceHarness, catalog.IngestOptions{LegacyWindsurfHooks: true})
						} else {
							cat, err = catalog.Load(root, tt.sourceHarness)
						}
						if err != nil {
							t.Fatal(err)
						}
						cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: root, CatalogKey: root, Source: config.RepoCommitted, Default: true})
						for _, entry := range cat.Entries {
							if entry.Kind != ir.KindHook || (tt.selectHook != nil && !tt.selectHook(entry.Resource)) {
								continue
							}
							if entry.SourceHarness != tt.sourceHarness {
								t.Fatalf("source harness = %q, want %q", entry.SourceHarness, tt.sourceHarness)
							}
							catalogs[root] = append(catalogs[root], entry)
						}
					}
					resolved, errs := resolver.Resolve(cfg, catalogs, nil, resolver.WithTargetHarnesses([]hcc.Target{hcc.TargetWindsurf}))
					if len(errs) != 0 || len(resolved) != 2 {
						t.Fatalf("resolution: %v, %v", resolved, errs)
					}
					report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: mode}))
					if err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("materialize: %v %v", err, report.Diags)
					}
					got := additiveSafetyCommands(t, filepath.Join(repo, ".devin", "hooks.json"), "hooks", "pre_run_command")
					if !reflect.DeepEqual(got, []string{"echo first", "echo second"}) {
						t.Fatalf("commands = %v", got)
					}
					supportDir := filepath.Join(repo, "hook-support")
					// Support payloads install under per-source namespaces
					// (<key>/<hook name>/); compare basenames across the tree.
					payloads := map[string]string{}
					walkErr := filepath.WalkDir(supportDir, func(p string, d fs.DirEntry, err error) error {
						if err != nil || d.IsDir() {
							return err
						}
						data, rerr := os.ReadFile(p)
						if rerr != nil {
							return rerr
						}
						payloads[filepath.Base(p)] = string(data)
						return nil
					})
					if walkErr != nil {
						t.Fatalf("walk support dir: %v", walkErr)
					}
					for _, name := range tt.excluded {
						if _, ok := payloads[name]; ok {
							t.Fatalf("excluded support file %s exists under %s", name, supportDir)
						}
					}
					for i, name := range []string{"first.sh", "second.sh"} {
						if data, ok := payloads[name]; !ok || data != "echo support "+strconv.Itoa(i) {
							t.Fatalf("support %s = %q", name, data)
						}
					}
					if tt.wantPayload {
						if data, ok := payloads["payload.json"]; !ok || data != `{"key":"value"}` {
							t.Fatalf("payload = %q", data)
						}
					}
				})
			}
		})
	}
}

func addHookSupportFiles(t *testing.T, dir string, index int) {
	additiveSafetyWrite(t, filepath.Join(dir, []string{"first.sh", "second.sh"}[index]), "echo support "+strconv.Itoa(index))
	additiveSafetyWrite(t, filepath.Join(dir, "payload.json"), `{"key":"value"}`)
}

func TestWindsurfNativeHandlerOrder(t *testing.T) {
	tests := []struct {
		name   string
		count  int
		seeded bool
	}{
		{name: "fresh-3", count: 3},
		{name: "fresh-12", count: 12},
		{name: "seeded-3", count: 3, seeded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, _ := additiveSafetyEnv(t)
			root := t.TempDir()
			handlers := make([]map[string]string, 0, tt.count)
			want := make([]string, 0, tt.count)
			for i := 0; i < tt.count; i++ {
				command := "echo " + strconv.Itoa(i)
				handlers = append(handlers, map[string]string{"command": command})
				want = append(want, command)
			}
			body, err := json.Marshal(map[string]any{"hooks": map[string]any{"pre_run_command": handlers}})
			if err != nil {
				t.Fatal(err)
			}
			additiveSafetyWrite(t, filepath.Join(root, ".windsurf", "hooks.json"), string(body))
			cat, err := catalog.Load(root, "windsurf")
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range cat.Entries {
				if entry.Kind != ir.KindHook {
					continue
				}
				hook := entry.Resource.(*ir.Hook)
				index, err := strconv.Atoi(strings.TrimPrefix(hook.Command, "echo "))
				if err != nil {
					t.Fatal(err)
				}
				wantName := "hooks-" + strconv.Itoa(index+2)
				if index == tt.count-1 {
					wantName = "hooks"
				}
				if entry.Name != wantName {
					t.Fatalf("source binding for %q = %q, want %q", hook.Command, entry.Name, wantName)
				}
			}
			cfg := &config.Config{Registries: []config.RegistryEntry{{URL: root, CatalogKey: root, Source: config.RepoCommitted, Default: true}}}
			resolved, errs := resolver.Resolve(cfg, map[string][]catalog.Entry{root: cat.Entries}, nil, resolver.WithTargetHarnesses([]hcc.Target{hcc.TargetWindsurf}))
			if len(errs) != 0 || len(resolved) != tt.count {
				t.Fatalf("resolution: %v, %v", resolved, errs)
			}
			path := filepath.Join(repo, ".devin", "hooks.json")
			if tt.seeded {
				additiveSafetyWrite(t, path, fmt.Sprintf(`{"hooks":{"pre_run_command":[// keep handwritten
{"command":"echo handwritten"},{"command":"echo %d","_managed_by":"agent-marketplace"}]}}`, tt.count-1))
			}
			report, err := Run(context.Background(), repo, resolved)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("materialize: %v %v", err, report.Diags)
			}
			first, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			commandsPath := path
			if tt.seeded {
				commandsPath = filepath.Join(t.TempDir(), "hooks.json")
				additiveSafetyWrite(t, commandsPath, strings.ReplaceAll(string(first), "// keep handwritten", ""))
			}
			got := additiveSafetyCommands(t, commandsPath, "hooks", "pre_run_command")
			wantCommands := want
			if tt.seeded {
				wantCommands = append([]string{"echo handwritten", "echo " + strconv.Itoa(tt.count-1)}, want[:tt.count-1]...)
				if !strings.Contains(string(first), "// keep handwritten") {
					t.Fatal("handwritten hook comment was lost")
				}
				store, err := ownership.Open(repo)
				if err != nil {
					t.Fatal(err)
				}
				proof, err := store.Proof(filepath.Join(".devin", "hooks.json"))
				if err != nil {
					t.Fatal(err)
				}
				if proof.OwnedCount() != tt.count-1 || len(proof.HookSatisfactions()) != 1 {
					t.Fatalf("handwritten marker satisfaction became owned: %d owned, %d satisfactions", proof.OwnedCount(), len(proof.HookSatisfactions()))
				}
				sources := proof.HookSatisfactions()[0].Sources
				if len(sources) != 1 || sources[0].Resource != "hooks" || sources[0].SourceDigest == "" {
					t.Fatalf("handwritten marker satisfaction sources = %+v", sources)
				}
			}
			if !reflect.DeepEqual(got, wantCommands) {
				t.Fatalf("commands = %v, want %v", got, wantCommands)
			}
			report, err = Run(context.Background(), repo, resolved)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("repeat materialize: %v %v", err, report.Diags)
			}
			second, err := os.ReadFile(path)
			if err != nil || !reflect.DeepEqual(first, second) {
				t.Fatalf("repeat sync changed bytes: %v", err)
			}
			dry, err := Run(context.Background(), repo, resolved, WithDryRun())
			if err != nil || hasErrorDiagnostics(dry.Diags) {
				t.Fatalf("dry run: %v %v", err, dry.Diags)
			}
			written := 0
			for _, target := range dry.PerTarget {
				written += len(target.Written)
			}
			if written != 0 {
				t.Fatalf("dry run wrote %d paths", written)
			}
		})
	}
}
