package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestSupportedHarnessTargets(t *testing.T) {
	want := targetmeta.CompilerNames()
	got := SupportedHarnessTargets()
	if !slices.Equal(got, want) {
		t.Fatalf("SupportedHarnessTargets() = %v, want registry compiler targets %v", got, want)
	}
	if !slices.Contains(got, string(targetmeta.TargetDevin)) {
		t.Fatal("SupportedHarnessTargets() omits Devin")
	}
	if !slices.Contains(got, string(targetmeta.TargetCodex)) {
		t.Fatal("SupportedHarnessTargets() omits Codex")
	}
}

func TestIsSupportedHarness(t *testing.T) {
	for _, id := range SupportedHarnessTargets() {
		if !IsSupportedHarness(id) {
			t.Errorf("IsSupportedHarness(%q) = false, want true", id)
		}
	}
	if IsSupportedHarness("unknown") {
		t.Error("IsSupportedHarness(\"unknown\") = true, want false")
	}
}

func TestValidateHarnessIDs(t *testing.T) {
	if err := ValidateHarnessIDs([]string{"factory", "claude", "codex"}); err != nil {
		t.Errorf("ValidateHarnessIDs valid = %v", err)
	}
	if err := ValidateHarnessIDs([]string{"factory", "  "}); err == nil {
		t.Error("ValidateHarnessIDs empty value = nil, want error")
	}
	if err := ValidateHarnessIDs([]string{"unknown"}); err == nil {
		t.Error("ValidateHarnessIDs unknown = nil, want error")
	}
}

func TestDetectInstalledHarnessesUsesCodexHomeNotSharedAgentsRoot(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", codexHome)

	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := harness.DetectInstalled(home)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, "codex") {
		t.Fatalf("shared .agents root alone detected Codex: %v", got)
	}

	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = harness.DetectInstalled(home)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "codex") {
		t.Fatalf("resolved CODEX_HOME did not detect Codex: %v", got)
	}
}

func TestDetectInstalledHarnessesUsesDefaultCodexHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := harness.DetectInstalled(home)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "codex") {
		t.Fatalf("default CODEX_HOME did not detect Codex: %v", got)
	}
}

func TestDetectInstalledHarnessesInHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")

	// None detected initially.
	got, err := harness.DetectInstalled(home)
	if err != nil {
		t.Fatalf("DetectInstalledHarnessesInHome error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}

	// Create a factory and gemini root.
	if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err = harness.DetectInstalled(home)
	if err != nil {
		t.Fatalf("DetectInstalledHarnessesInHome error: %v", err)
	}
	want := []string{"factory", "gemini"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("DetectInstalledHarnessesInHome()[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestInitializeUserPersonalAtPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Creates a minimal v4 user personal config.
	written, err := InitializeUserPersonalAtPath(path, []string{"factory"})
	if err != nil {
		t.Fatalf("InitializeUserPersonalAtPath error: %v", err)
	}
	if written != path {
		t.Fatalf("path = %q, want %q", written, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: agent-marketplace/v4") {
		t.Errorf("config missing v4 apiVersion: %s", content)
	}
	if !strings.Contains(content, "harnesses:") {
		t.Errorf("config missing harnesses: %s", content)
	}
	if !strings.Contains(content, "- factory") {
		t.Errorf("config missing factory harness: %s", content)
	}
	if strings.Contains(content, "registries:") || strings.Contains(content, "logging:") {
		t.Errorf("config contains implicit registry or logging: %s", content)
	}

	// Re-running is a safe no-op.
	_, err = InitializeUserPersonalAtPath(path, []string{"claude"})
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("expected ErrAlreadyInitialized, got %v", err)
	}

	// Existing file should be unchanged.
	data2, _ := os.ReadFile(path)
	if string(data2) != content {
		t.Errorf("existing config was modified after second init")
	}
}

func TestInitializeUserPersonalAtPathRejectsUnknownHarness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if _, err := InitializeUserPersonalAtPath(path, []string{"skynet"}); err == nil {
		t.Fatal("expected error for unknown harness")
	}
}

func TestInitializeUserPersonalAtPathRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if _, err := InitializeUserPersonalAtPath(path, []string{}); err == nil {
		t.Fatal("expected error for empty harnesses")
	}
}

func TestInitializeUserPersonalAtPathNormalizesHarnesses(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name      string
		harnesses []string
	}{
		{"duplicates", []string{"factory", "factory"}},
		{"padded", []string{" factory "}},
		{"mixed-duplicates-and-padded", []string{" factory ", "factory", " factory "}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".yaml")
			if _, err := InitializeUserPersonalAtPath(path, tc.harnesses); err != nil {
				t.Fatalf("InitializeUserPersonalAtPath error: %v", err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			content := string(data)
			if strings.Count(content, "- factory") != 1 {
				t.Fatalf("expected exactly one '- factory' entry, got:\n%s", content)
			}
		})
	}
}

func TestUserPersonalConfigExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	exists, err := UserPersonalConfigExists()
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("UserPersonalConfigExists = true, want false")
	}

	cfgPath, err := UserPersonalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exists, err = UserPersonalConfigExists()
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("UserPersonalConfigExists = false, want true")
	}
}
