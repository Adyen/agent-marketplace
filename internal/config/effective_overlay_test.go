package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEffectiveFileOverlaysRetainRealPaths(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	repo := filepath.Join(dir, "repo.yaml")
	local := filepath.Join(dir, "local.yaml")
	if err := os.WriteFile(user, []byte("apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.test/shared.git, ref: main}}\nresources: {shared: [skill/old]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       user, RepoPath: repo, LocalPath: local,
		FileOverlays: map[string][]byte{
			user:  []byte("apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.test/shared.git, ref: main}}\nresources: {shared: [skill/user-new]}\n"),
			repo:  []byte("apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.test/shared.git, ref: main}}\nresources: {shared: [skill/repo-new]}\n"),
			local: []byte("apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.test/shared.git, ref: main}}\nresources: {shared: [skill/local-new]}\n"),
		},
	})
	if len(ec.Errors) != 0 {
		t.Fatalf("errors = %v", ec.Errors)
	}
	if ec.User.Path != user || ec.Repo.Path != repo || ec.LocalLayer.Path != local {
		t.Fatalf("paths changed: %#v %#v %#v", ec.User, ec.Repo, ec.LocalLayer)
	}
	if ec.User.Config == nil || ec.Repo.Config == nil || ec.Local == nil {
		t.Fatalf("overlaid layers missing: %#v", ec)
	}
	for _, layer := range []struct {
		entries []RegistryEntry
		skill   string
	}{
		{ec.User.Config.Registries, "user-new"}, {ec.Repo.Config.Registries, "repo-new"}, {ec.Local.Registries, "local-new"},
	} {
		if len(layer.entries) != 1 || len(layer.entries[0].Skills.Include) != 1 || layer.entries[0].Skills.Include[0] != layer.skill {
			t.Fatalf("overlaid selections missing: entries=%#v want=%s", layer.entries, layer.skill)
		}
	}
	raw, err := os.ReadFile(user)
	if err != nil || string(raw) != "apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.test/shared.git, ref: main}}\nresources: {shared: [skill/old]}\n" {
		t.Fatalf("real user config changed: %q, %v", raw, err)
	}
	if _, err := os.Stat(repo); !os.IsNotExist(err) {
		t.Fatalf("absent repo config created: %v", err)
	}
}

func TestLoadEffectiveV4MissingLocalOverlayThroughAlias(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "actual")
	if err := os.MkdirAll(filepath.Join(actual, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(actual)
	if err != nil {
		t.Fatal(err)
	}
	local := LocalConfigPath(alias)
	canonical := LocalConfigPath(resolved)
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatalf("local config already exists: %v", err)
	}
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		RepoRoot: alias, LocalConfigDir: alias,
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       filepath.Join(dir, "missing-user.yaml"),
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        local,
		FileOverlays:     map[string][]byte{canonical: []byte("apiVersion: agent-marketplace/v4\n")},
	})
	if len(ec.Errors) != 0 || ec.Local == nil || ec.Local.APIVersion != APIVersionV4 {
		t.Fatalf("v4 local overlay not loaded: local=%#v errors=%v", ec.Local, ec.Errors)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatalf("overlay wrote local config: %v", err)
	}
}

func TestLoadEffectiveV4RootUsesSuppliedOverlay(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	onDisk := []byte(`apiVersion: agent-marketplace/v4
sources: {old: {git: https://example.test/old.git, ref: main}}
resources: {old: [skill/old]}
`)
	overlay := []byte(`apiVersion: agent-marketplace/v4
sources: {new: {git: https://example.test/new.git, ref: main}}
resources: {new: [skill/new]}
`)
	if err := os.WriteFile(user, onDisk, 0o600); err != nil {
		t.Fatal(err)
	}
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       user,
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		FileOverlays:     map[string][]byte{user: overlay},
	})
	if len(ec.Errors) != 0 {
		t.Fatalf("errors = %v", ec.Errors)
	}
	if ec.User.Config == nil || len(ec.User.Config.Registries) != 1 || ec.User.Config.Registries[0].URL != "https://example.test/new.git" {
		t.Fatalf("v4 overlay ignored: %#v", ec.User.Config)
	}
	if got, err := os.ReadFile(user); err != nil || string(got) != string(onDisk) {
		t.Fatalf("overlay changed disk root: %q, %v", got, err)
	}
}

func TestLoadEffectiveNeverOverlaysOrganization(t *testing.T) {
	dir := t.TempDir()
	org := filepath.Join(dir, "org.yaml")
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: org,
		GlobalPath:       filepath.Join(dir, "missing-user.yaml"),
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		FileOverlays:     map[string][]byte{org: []byte("apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.com/x.git, ref: main}}\nresources: {shared: [skill/review]}\n")},
	})
	if ec.Organization.Config != nil || ec.Managed {
		t.Fatalf("organization overlay bypassed trust: %#v", ec.Organization)
	}
}
