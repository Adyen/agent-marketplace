package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestLoadEffectiveFileOverlaysRetainRealPaths(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	repo := filepath.Join(dir, "repo.yaml")
	local := filepath.Join(dir, "local.yaml")
	if err := os.WriteFile(user, []byte("apiVersion: agent-marketplace/v2\nskills: [old]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ec := LoadEffective(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       user, RepoPath: repo, LocalPath: local,
		FileOverlays: map[string][]byte{
			user:  []byte("apiVersion: agent-marketplace/v2\nskills: [user-new]\n"),
			repo:  []byte("apiVersion: agent-marketplace/v2\nskills: [repo-new]\n"),
			local: []byte("skills: [local-new]\n"),
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
	raw, err := os.ReadFile(user)
	if err != nil || string(raw) != "apiVersion: agent-marketplace/v2\nskills: [old]\n" {
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
	ec := LoadEffective(context.Background(), LoadEffectiveOptions{
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
	ec := LoadEffective(context.Background(), LoadEffectiveOptions{
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
	ec := LoadEffective(context.Background(), LoadEffectiveOptions{
		OrganizationPath: org,
		GlobalPath:       filepath.Join(dir, "missing-user.yaml"),
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		FileOverlays:     map[string][]byte{org: []byte("apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.com/x.git\n    ref: main\n")},
	})
	if ec.Organization.Config != nil || ec.Managed {
		t.Fatalf("organization overlay bypassed trust: %#v", ec.Organization)
	}
}

func TestLoadEffectiveUsesOnlyOrganizationRuntimeLockOverlay(t *testing.T) {
	dir := t.TempDir()
	org := filepath.Join(dir, "config.yaml")
	onDisk := []byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: https://marketplace.example}
resources:
  hosted: {include: [tools/skill/review]}
`)
	if err := os.WriteFile(org, onDisk, 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "lock.yaml")
	lockData, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeOrganization,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL:              "https://marketplace.example",
			InstanceID:       "marketplace",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "registry"}},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName:  "tools",
				RegistryID:    "registry",
				Kind:          "skill",
				Name:          "review",
				Revision:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ContentDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ArchiveDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Acquisition:   lockfile.MarketplaceAcquisitionMirror,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ec := LoadEffective(context.Background(), LoadEffectiveOptions{
		OrganizationPath:        org,
		GlobalPath:              filepath.Join(dir, "missing-user.yaml"),
		RepoPath:                filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:               filepath.Join(dir, "missing-local.yaml"),
		OrganizationRuntimeLock: lockData,
	})
	if len(ec.Errors) != 0 {
		t.Fatalf("errors = %v", ec.Errors)
	}
	if ec.Organization.Config == nil || len(ec.Organization.Config.Marketplaces) != 1 || ec.Organization.Config.Marketplaces[0].InstanceID != "marketplace" {
		t.Fatalf("runtime lock was not applied: %#v", ec.Organization.Config)
	}
	runtimeRead := false
	for _, read := range ec.Organization.Reads {
		if (read.Path == lockPath || read.RequestedPath == lockPath) && read.Overlay && read.RuntimeLock {
			runtimeRead = true
		}
	}
	if !runtimeRead {
		t.Fatalf("runtime lock read was not distinguished from a staged config overlay: %#v", ec.Organization.Reads)
	}
	if got, err := os.ReadFile(org); err != nil || string(got) != string(onDisk) {
		t.Fatalf("runtime lock changed organization config: %q, %v", got, err)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("runtime lock was published: %v", err)
	}
}
