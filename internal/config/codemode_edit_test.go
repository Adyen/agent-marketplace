package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV4ParsesCodeMode(t *testing.T) {
	cfg, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\ncodeMode:\n  enabled: true\n"), "config.yaml", true)
	if err != nil {
		t.Fatalf("ParseV4Config: %v", err)
	}
	if cfg.CodeMode == nil || !cfg.CodeMode.EnabledValue() {
		t.Fatalf("CodeMode = %+v", cfg.CodeMode)
	}
}

func TestV4RejectsUnknownCodeModeField(t *testing.T) {
	_, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\ncodeMode:\n  enabled: true\n  extra: 1\n"), "config.yaml", true)
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
}

// spec: code-mode-mcp/layer-precedence-resolves-effective-enable
func TestCodeModeMergePrecedence(t *testing.T) {
	tru := true
	fal := false
	org := Layer{Source: Organization, Config: &Config{CodeMode: &CodeModeConfig{Enabled: &fal}}}
	user := Layer{Source: UserPersonal, Config: &Config{CodeMode: &CodeModeConfig{Enabled: &tru}}}
	merged := MergeLayers([]Layer{org, user})
	if !merged.CodeMode.EnabledValue() {
		t.Fatal("organization false force-disabled a declaring lower layer")
	}
	merged = MergeLayers([]Layer{org})
	if merged.CodeMode.EnabledValue() {
		t.Fatal("organization false did not apply when nothing else declared")
	}
	if MergeLayers(nil).CodeMode.EnabledValue() {
		t.Fatal("code mode enabled with no declarations")
	}
}

func TestSetAndRemoveCodeMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetCodeModeEnabled(path, true); err != nil {
		t.Fatalf("SetCodeModeEnabled: %v", err)
	}
	cfg, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadV4ConfigFromPath: %v", err)
	}
	if !cfg.CodeMode.EnabledValue() {
		t.Fatal("enabled not persisted")
	}
	if err := RemoveCodeMode(path); err != nil {
		t.Fatalf("RemoveCodeMode: %v", err)
	}
	cfg, err = LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadV4ConfigFromPath: %v", err)
	}
	if cfg.CodeMode != nil {
		t.Fatalf("codeMode block not removed: %+v", cfg.CodeMode)
	}
}

func TestSetCodeModeEnabledCreatesV4Roots(t *testing.T) {
	for _, name := range []string{"config.yaml", "config.local.yaml"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := SetCodeModeEnabled(path, true); err != nil {
				t.Fatalf("SetCodeModeEnabled: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if PeekAPIVersion(data) != APIVersionV4 {
				t.Fatalf("fresh root is not v4:\n%s", data)
			}
			cfg, err := LoadV4ConfigFromPath(path)
			if err != nil {
				t.Fatalf("LoadV4ConfigFromPath: %v", err)
			}
			if !cfg.CodeMode.EnabledValue() {
				t.Fatal("codeMode.enabled not persisted")
			}
		})
	}
}

func TestSetCodeModeEnabledRefusesLegacyRoots(t *testing.T) {
	bodies := map[string]string{
		"v2.yaml":           "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n",
		"v3.yaml":           "apiVersion: agent-marketplace/v3\nharnesses: [factory]\n",
		"noversion.yaml":    "harnesses: [factory]\n",
		"config.local.yaml": "registries: []\n",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			err := SetCodeModeEnabled(path, true)
			if err == nil || !strings.Contains(err.Error(), "migrate --scope") {
				t.Fatalf("expected migrate guidance, got %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != body {
				t.Fatalf("legacy root modified:\n%s", after)
			}
		})
	}
}

func TestRemoveCodeModeAbsentRootIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := RemoveCodeMode(path); err != nil {
		t.Fatalf("RemoveCodeMode: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("disable created a config root")
	}
}

func TestRemoveCodeModeLeavesLegacyRootUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCodeMode(path); err != nil {
		t.Fatalf("RemoveCodeMode: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != body {
		t.Fatalf("legacy root modified:\n%s", after)
	}
}
