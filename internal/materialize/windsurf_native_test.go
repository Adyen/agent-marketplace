package materialize

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pkgcatalog "github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestWindsurfNativeHooksIngestResolveMaterialize(t *testing.T) {
	for _, machineScope := range []bool{false, true} {
		for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
			t.Run(string(config.MaterializationMode(mode))+"/machine="+boolString(machineScope), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				repo := t.TempDir()
				cfg := &config.Config{Harnesses: []string{"windsurf"}}
				catalogs := make(map[string][]pkgcatalog.Entry)
				for i, command := range []string{"echo first", "echo second"} {
					registry := filepath.Join(t.TempDir(), "registry", string(rune('a'+i)))
					hooksPath := filepath.Join(registry, ".windsurf", "hooks.json")
					if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(hooksPath, []byte(`{"hooks":{"pre_run_command":[{"command":"`+command+`"}]}}`), 0o644); err != nil {
						t.Fatal(err)
					}
					cat, err := pkgcatalog.Load(registry, pkgcatalog.HarnessWindsurf)
					if err != nil {
						t.Fatal(err)
					}
					source := config.RepoCommitted
					if machineScope {
						source = config.UserPersonal
					}
					cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: registry, Source: source, Default: true, CatalogKey: registry})
					catalogs[registry] = cat.Entries
				}
				resolved, errs := resolver.Resolve(cfg, catalogs, nil, resolver.WithTargetHarnesses([]hcc.Target{hcc.TargetWindsurf}))
				if len(errs) != 0 || len(resolved) != 2 {
					t.Fatalf("Resolve = %d artifacts, errors=%v", len(resolved), errs)
				}
				initial := `{"hooks":{"pre_run_command":[{"command":"echo handwritten"}],"other":[{"command":"echo unrelated"}]}}`
				nativePath := filepath.Join(repo, ".windsurf", "hooks.json")
				if machineScope {
					nativePath = filepath.Join(home, ".codeium", "windsurf", "hooks.json")
				}
				if err := os.MkdirAll(filepath.Dir(nativePath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(nativePath, []byte(initial), 0o644); err != nil {
					t.Fatal(err)
				}
				report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
				if err != nil || hasErrorDiagnostics(report.Diags) {
					t.Fatalf("Run = %v diagnostics=%v", err, report.Diags)
				}
				first, err := os.ReadFile(nativePath)
				if err != nil {
					t.Fatal(err)
				}
				assertWindsurfNativeCommands(t, first, []string{"echo handwritten", "echo first", "echo second"})
				if _, err := Run(context.Background(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})); err != nil {
					t.Fatal(err)
				}
				second, _ := os.ReadFile(nativePath)
				if !reflect.DeepEqual(first, second) {
					t.Fatal("second sync changed native hook bytes")
				}
				if _, err := Run(context.Background(), repo, resolved[1:], WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})); err != nil {
					t.Fatal(err)
				}
				remaining, _ := os.ReadFile(nativePath)
				assertWindsurfNativeCommands(t, remaining, []string{"echo handwritten", "echo second"})
			})
		}
	}
}

func assertWindsurfNativeCommands(t *testing.T, data []byte, want []string) {
	t.Helper()
	var document struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, group := range document.Hooks["pre_run_command"] {
		got = append(got, group.Command)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native commands = %v, want %v", got, want)
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
