package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func TestLoadAndResolveSettingsIgnoresUnreadableLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	setStateRoot(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	if err := os.WriteFile(orgPath, []byte("apiVersion: agent-marketplace/v4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	amconfig.SetTestOrganizationConfigPath(t, orgPath)

	agentsDir := filepath.Join(home, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(`apiVersion: agent-marketplace/v4
telemetry: {level: basic}
sources: {hosted: {marketplace: https://marketplace.invalid}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsDir, "lock.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	state, err := LoadAndResolve(context.Background())
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	if state.EffectiveLevel != tmcfg.LevelBasic || state.EffectiveLevelSource != sourceUser {
		t.Fatalf("effective level = %q/%q, want basic/USER", state.EffectiveLevel, state.EffectiveLevelSource)
	}
}
