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
