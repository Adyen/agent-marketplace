//go:build !windows

package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestReadOnlyOrganizationLockViewAcceptsMissingLock(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	lockPath := filepath.Join(dir, "lock.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := config.RegistryEntry{URL: "https://example.invalid/org.git", Ref: "main", Source: config.Organization}
	doc, err := readOnlyLayerLock(context.Background(), config.LayerResult{Path: configPath}, lockPath, lockfile.ScopeOrganization)
	if err != nil || doc.APIVersion != "" {
		t.Fatalf("missing optional organization lock returned doc=%#v err=%v", doc, err)
	}
	effective := config.EffectiveConfig{
		Organization: config.LayerResult{Source: config.Organization, Path: configPath, Config: &config.Config{Registries: []config.RegistryEntry{registry}}},
	}
	_, view := exactLockViewWithContext(context.Background(), effective, false)
	defer view.release()
	if len(view.errors) != 0 {
		t.Fatalf("unlocked organization view errors = %v", view.errors)
	}
	path, identity, err := view.exactSnapshot(registry)
	if err != nil || path != "" || identity != "" {
		t.Fatalf("unlocked organization snapshot = (%q, %q, %v), want mutable discovery", path, identity, err)
	}
}

func TestReadOnlyLockViewAllowsMissingRepoLockWhenRequested(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	registry := config.RegistryEntry{URL: "https://example.invalid/repo.git", Ref: "main", Source: config.RepoCommitted}
	effective := config.EffectiveConfig{
		Repo: config.LayerResult{Source: config.RepoCommitted, Path: configPath, Config: &config.Config{Registries: []config.RegistryEntry{registry}}},
	}
	_, view := exactLockViewWithContext(context.Background(), effective, true)
	defer view.release()
	if len(view.errors) != 0 {
		t.Fatalf("missing repo lock returned errors = %v", view.errors)
	}
	if len(view.warnings) != 1 {
		t.Fatalf("missing repo lock warnings = %v, want one warning", view.warnings)
	}
	path, identity, err := view.exactSnapshot(registry)
	if err != nil || path != "" || identity != "" {
		t.Fatalf("missing repo lock snapshot = (%q, %q, %v), want mutable discovery", path, identity, err)
	}
}

func TestReadOnlyOrganizationLockViewAcceptsTrustedFixture(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	lockPath := filepath.Join(dir, "lock.yaml")
	for path, data := range map[string]string{
		configPath: "apiVersion: agent-marketplace/v2\nregistries: []\n",
		lockPath:   "apiVersion: agent-marketplace-lock/v1\nscope: organization\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := readOnlyLayerLock(context.Background(), config.LayerResult{Path: configPath}, lockPath, lockfile.ScopeOrganization)
	if err != nil || doc.Scope != lockfile.ScopeOrganization {
		t.Fatalf("trusted read-only organization view failed: scope=%s err=%v", doc.Scope, err)
	}
}
