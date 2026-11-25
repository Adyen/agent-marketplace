package config

import (
	"os"
	"strings"
	"testing"
)

// spec: gui-configuration/editing-a-section-keeps-comments-and-unrelated-keys
func TestSetUserSettingsPreservesCommentsAndUnrelatedKeys(t *testing.T) {
	path := setupUserConfig(t)
	writeTestFile(t, path, []byte("apiVersion: agent-marketplace/v4\n# keep me\nharnesses:\n  - factory\n"))

	if err := SetUserSettings(path, "backup", map[string]any{"max_days": 7}); err != nil {
		t.Fatalf("SetUserSettings: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"# keep me", "harnesses:", "backup:", "max_days: 7"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q in:\n%s", want, content)
		}
	}
}

// spec: gui-configuration/clearing-the-last-key-removes-the-section
func TestSetUserSettingsClearingRemovesKeyAndEmptySection(t *testing.T) {
	path := setupUserConfig(t)
	if err := SetUserSettings(path, "logging", map[string]any{"level": "warn"}); err != nil {
		t.Fatalf("set logging level: %v", err)
	}
	if err := SetUserSettings(path, "logging", map[string]any{"level": nil}); err != nil {
		t.Fatalf("clear logging level: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "logging:") {
		t.Fatalf("expected empty logging section removed, got:\n%s", data)
	}
}

// spec: gui-configuration/save-refuses-an-invalid-value-without-writing
func TestSetUserSettingsRefusesInvalidValuesWithoutWrite(t *testing.T) {
	path := setupUserConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		section string
		fields  map[string]any
	}{
		{"backup", map[string]any{"max_days": -1}},
		{"logging", map[string]any{"max_size_mb": 0}},
		{"logging", map[string]any{"level": "trace"}},
		{"logging", map[string]any{"file": "  "}},
		{"materialization", map[string]any{"mode": "hardlink"}},
		{"materialization", map[string]any{"targets": map[string]string{"nope": "file"}}},
	}
	for _, tc := range cases {
		if err := SetUserSettings(path, tc.section, tc.fields); err == nil {
			t.Fatalf("SetUserSettings(%s, %v) accepted invalid value", tc.section, tc.fields)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Fatalf("invalid %s value changed the file:\n%s", tc.section, after)
		}
	}
}

func TestSetUserSettingsRoundTripsThroughLoader(t *testing.T) {
	path := setupUserConfig(t)
	if err := SetUserSettings(path, "backup", map[string]any{"max_days": 7, "enabled": false}); err != nil {
		t.Fatalf("SetUserSettings: %v", err)
	}
	cfg, err := LoadRootConfigFromPath(path)
	if err != nil {
		t.Fatalf("reload user config: %v", err)
	}
	if cfg.Backup == nil || cfg.Backup.MaxDays == nil || *cfg.Backup.MaxDays != 7 {
		t.Fatalf("expected backup.max_days 7, got %+v", cfg.Backup)
	}
	if cfg.Backup.Enabled == nil || *cfg.Backup.Enabled {
		t.Fatalf("expected backup.enabled false, got %+v", cfg.Backup)
	}
}
