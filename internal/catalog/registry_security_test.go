package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestDiscoverRejectsSkillSymlinkOutsideRegistry(t *testing.T) {
	registry := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "sentinel"), []byte("must not be copied"), 0o600); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(registry, "skills", "external")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "sentinel"), filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	loaded, err := Discover(registry, "claude")
	if err == nil || !strings.Contains(err.Error(), "resolves outside the registry root") {
		t.Fatalf("Discover error = %v, want skill symlink containment rejection", err)
	}
	if len(loaded.Entries) != 0 || len(loaded.Resources) != 0 {
		t.Fatalf("rejected registry returned resources: entries=%v resources=%v", loaded.Entries, loaded.Resources)
	}
}

func TestLoadRegistriesRejectsRemoteSymlink(t *testing.T) {
	registry := t.TempDir()
	target := filepath.Join(registry, "target")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(registry, "link")); err != nil {
		t.Fatal(err)
	}

	result := LoadRegistries(&config.Config{Registries: []config.RegistryEntry{{
		URL:           "https://example.invalid/registry.git",
		Ref:           "main",
		SourceHarness: "claude",
	}}}, "", LoadModeRefresh, WithCloneOrPull(func(_, _ string) (string, error) {
		return registry, nil
	}))
	if len(result.Errors) == 0 {
		t.Fatal("LoadRegistries accepted a remote registry containing a symlink")
	}
	if !strings.Contains(result.Errors[0].Error(), "remote registries do not allow symlinks") {
		t.Fatalf("LoadRegistries error = %v, want actionable remote symlink rejection", result.Errors[0])
	}
}
