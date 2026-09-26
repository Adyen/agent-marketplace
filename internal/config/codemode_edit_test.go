package config

import (
	"os"
	"path/filepath"
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
