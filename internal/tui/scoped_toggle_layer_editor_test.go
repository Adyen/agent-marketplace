package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func scopedToggleConfigForScope(t *testing.T, repoRoot string, s scope) string {
	t.Helper()
	switch s {
	case scopeGlobal:
		path, err := config.GlobalConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		return path
	case scopeProject:
		return config.ProjectConfigPath(repoRoot)
	default:
		return config.LocalConfigPath(repoRoot)
	}
}

func runToggleAndErr(t *testing.T, repoRoot string, act toggleAction) error {
	t.Helper()
	msg, ok := runScopedToggle(repoRoot, act)().(configWrittenMsg)
	if !ok {
		t.Fatal("expected configWrittenMsg")
	}
	return msg.err
}

func registrySelectorIncludes(cfg *config.Config, kind ir.ResourceKind, name string) bool {
	for _, entry := range cfg.Registries {
		for _, selected := range entry.SelectorForKind(kind).Include {
			if selected == name {
				return true
			}
		}
	}
	return false
}

func registrySelectorExcludes(cfg *config.Config, kind ir.ResourceKind, name string) bool {
	for _, entry := range cfg.Registries {
		for _, excluded := range entry.SelectorForKind(kind).Exclude {
			if excluded == name {
				return true
			}
		}
	}
	return false
}

func TestScopedToggleUsesLayerEditorAcrossSchemas(t *testing.T) {
	schemas := map[string]string{
		"v2": "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n",
		"v4": "apiVersion: agent-marketplace/v4\nsources:\n  main: {git: https://example.test/repo.git, ref: main}\n",
	}
	for _, schema := range []string{"v2", "v4"} {
		for _, s := range []scope{scopeGlobal, scopeProject, scopeLocal} {
			t.Run(schema+"/"+scopeName(s), func(t *testing.T) {
				repoRoot := t.TempDir()
				t.Setenv("HOME", t.TempDir())
				configPath := scopedToggleConfigForScope(t, repoRoot, s)
				if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
					t.Fatal(err)
				}
				fixture := schemas[schema]
				if err := os.WriteFile(configPath, []byte(fixture), 0o640); err != nil {
					t.Fatal(err)
				}
				act := toggleAction{skillName: "audit", kind: ir.KindSkill, scope: s, registryURL: "https://example.test/repo.git", registryRef: "main"}

				act.enable = true
				if err := runToggleAndErr(t, repoRoot, act); err != nil {
					t.Fatalf("enable: %v", err)
				}
				cfg, err := config.LoadRootConfigFromPath(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if !registrySelectorIncludes(cfg, ir.KindSkill, "audit") {
					t.Fatalf("audit not included: %#v", cfg.Registries)
				}

				act.enable = false
				if err := runToggleAndErr(t, repoRoot, act); err != nil {
					t.Fatalf("disable: %v", err)
				}
				cfg, err = config.LoadRootConfigFromPath(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if registrySelectorIncludes(cfg, ir.KindSkill, "audit") || !registrySelectorExcludes(cfg, ir.KindSkill, "audit") {
					t.Fatalf("audit not excluded: %#v", cfg.Registries)
				}

				act.enable = true
				if err := runToggleAndErr(t, repoRoot, act); err != nil {
					t.Fatalf("re-enable: %v", err)
				}
				cfg, err = config.LoadRootConfigFromPath(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if !registrySelectorIncludes(cfg, ir.KindSkill, "audit") || registrySelectorExcludes(cfg, ir.KindSkill, "audit") {
					t.Fatalf("audit not re-included: %#v", cfg.Registries)
				}

				data, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), "agent-marketplace/"+schema) {
					t.Fatalf("schema changed:\n%s", data)
				}
				if schema == "v4" && strings.Contains(string(data), "registries:") {
					t.Fatalf("legacy keys in v4 config:\n%s", data)
				}
				if schema == "v2" && strings.Contains(string(data), "sources:") {
					t.Fatalf("v4 keys in v2 config:\n%s", data)
				}
			})
		}
	}
}

func TestScopedToggleMissingRootCreatesV4(t *testing.T) {
	for _, s := range []scope{scopeGlobal, scopeProject, scopeLocal} {
		t.Run(scopeName(s), func(t *testing.T) {
			repoRoot := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			configPath := scopedToggleConfigForScope(t, repoRoot, s)
			act := toggleAction{skillName: "audit", kind: ir.KindSkill, scope: s, enable: true, registryURL: "https://example.test/repo.git", registryRef: "main"}
			if err := runToggleAndErr(t, repoRoot, act); err != nil {
				t.Fatalf("toggle: %v", err)
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("missing root was not created: %v", err)
			}
			text := string(data)
			if !strings.Contains(text, "apiVersion: agent-marketplace/v4") || strings.Contains(text, "registries:") {
				t.Fatalf("missing root is not canonical v4:\n%s", text)
			}
			hasHarnesses := strings.Contains(text, "harnesses:")
			if s == scopeGlobal && !hasHarnesses {
				t.Fatalf("user root missing default harnesses:\n%s", text)
			}
			if s != scopeGlobal && hasHarnesses {
				t.Fatalf("repo/local root gained harnesses:\n%s", text)
			}
			if !strings.Contains(text, "audit") {
				t.Fatalf("selection missing:\n%s", text)
			}
		})
	}
}
