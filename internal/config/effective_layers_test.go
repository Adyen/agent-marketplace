package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEffectiveConfigLayerResultsExposeExplicitPathsInPrecedenceOrder(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "organization.yaml"),
		filepath.Join(dir, "user.yaml"),
		filepath.Join(dir, "repo.yaml"),
		filepath.Join(dir, "local.yaml"),
	}
	writeFile(t, paths[0], "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	writeFile(t, paths[1], "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	writeFile(t, paths[2], "apiVersion: agent-marketplace/v2\n")
	writeFile(t, paths[3], "skills: [local-skill]\n")

	ec := LoadEffective(context.Background(), LoadEffectiveOptions{
		RepoRoot:         dir,
		LocalConfigDir:   dir,
		OrganizationPath: paths[0],
		GlobalPath:       paths[1],
		RepoPath:         paths[2],
		LocalPath:        paths[3],
	})
	if len(ec.Errors) != 0 {
		t.Fatalf("LoadEffective errors: %v", ec.Errors)
	}

	wantSources := []Source{Organization, UserPersonal, RepoCommitted, RepoPersonal}
	assertLayerResults(t, ec.LayerResults(), wantSources, paths)
	assertLayerResults(t, ec.PresentLayers(), wantSources, paths)
	if ec.LocalLayer.Local == nil || ec.LocalLayer.Local.Skills.Include[0] != "local-skill" {
		t.Fatalf("LocalLayer did not expose parsed local config: %#v", ec.LocalLayer)
	}
	if ec.Local != ec.LocalLayer.Local {
		t.Fatal("legacy Local field and LocalLayer.Local must expose the same config")
	}
}

func TestEffectiveConfigLayerResultsExposeDefaultPaths(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	repo := filepath.Join(dir, "repo")
	orgPath := filepath.Join(dir, "etc", "agent-marketplace", "config.yaml")
	userPath := filepath.Join(home, ".agents", "config.yaml")
	repoPath := filepath.Join(repo, ".agents", "config.yaml")
	localPath := filepath.Join(repo, ".agents", "config.local.yaml")
	for _, path := range []string{orgPath, userPath, repoPath} {
		writeFile(t, path, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	}
	writeFile(t, localPath, "skills: [local-skill]\n")
	t.Setenv("HOME", home)
	SetTestOrganizationConfigPath(t, orgPath)

	ec := LoadEffective(context.Background(), LoadEffectiveOptions{
		RepoRoot:       repo,
		LocalConfigDir: repo,
	})
	if len(ec.Errors) != 0 {
		t.Fatalf("LoadEffective errors: %v", ec.Errors)
	}
	assertLayerResults(t, ec.LayerResults(),
		[]Source{Organization, UserPersonal, RepoCommitted, RepoPersonal},
		[]string{orgPath, userPath, repoPath, localPath})
}

func TestEffectiveConfigUsesNearestMonorepoLocalLayerPath(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	repo := filepath.Join(dir, "repo")
	service := filepath.Join(repo, "services", "payments")
	writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	nearestLocalPath := filepath.Join(service, ".agents", "config.local.yaml")
	writeFile(t, nearestLocalPath, "skills: [payments-skill]\n")
	if err := os.MkdirAll(filepath.Join(service, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(service, "src")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	t.Setenv("HOME", home)
	SetTestOrganizationConfigPath(t, filepath.Join(dir, "missing-org.yaml"))
	ResetRepoRootCache()
	t.Cleanup(ResetRepoRootCache)

	ec := LoadEffective(context.Background(), LoadEffectiveOptions{})
	if len(ec.Errors) != 0 {
		t.Fatalf("LoadEffective errors: %v", ec.Errors)
	}
	canonicalService, err := filepath.EvalSymlinks(service)
	if err != nil {
		t.Fatal(err)
	}
	wantLocalPath := filepath.Join(canonicalService, ".agents", "config.local.yaml")
	if ec.LocalLayer.Path != wantLocalPath {
		t.Fatalf("LocalLayer.Path = %q, want nearest local path %q", ec.LocalLayer.Path, wantLocalPath)
	}
	if ec.LocalLayer.Local == nil || ec.LocalLayer.Local.Skills.Include[0] != "payments-skill" {
		t.Fatalf("nearest local config was not loaded: %#v", ec.LocalLayer)
	}
}

func TestEffectiveConfigAbsentLayersRetainResolvedPaths(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "missing-organization.yaml"),
		filepath.Join(dir, "missing-user.yaml"),
		filepath.Join(dir, "missing-repo.yaml"),
		filepath.Join(dir, "missing-local.yaml"),
	}
	ec := LoadEffective(context.Background(), LoadEffectiveOptions{
		RepoRoot:         dir,
		LocalConfigDir:   dir,
		OrganizationPath: paths[0],
		GlobalPath:       paths[1],
		RepoPath:         paths[2],
		LocalPath:        paths[3],
	})

	layers := ec.LayerResults()
	if len(layers) != 4 {
		t.Fatalf("LayerResults length = %d, want 4", len(layers))
	}
	for i, layer := range layers {
		if layer.Path != paths[i] {
			t.Errorf("LayerResults[%d].Path = %q, want %q", i, layer.Path, paths[i])
		}
	}
	if got := ec.PresentLayers(); len(got) != 0 {
		t.Fatalf("PresentLayers = %#v, want no present layers", got)
	}
	if ec.LocalLayer.Source != RepoPersonal {
		t.Fatalf("LocalLayer.Source = %v, want %v", ec.LocalLayer.Source, RepoPersonal)
	}
}

func TestSourceLockMetadata(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		source   Source
		scope    string
		filename string
	}{
		{Organization, "organization", "lock.yaml"},
		{UserPersonal, "user", "lock.yaml"},
		{RepoCommitted, "repo", "lock.yaml"},
		{RepoPersonal, "local", "lock.local.yaml"},
		{SourceUnset, "", ""},
		{Source(99), "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.source.String(), func(t *testing.T) {
			if got := tc.source.LockScope(); got != tc.scope {
				t.Errorf("LockScope() = %q, want %q", got, tc.scope)
			}
			if got := tc.source.LockFilename(); got != tc.filename {
				t.Errorf("LockFilename() = %q, want %q", got, tc.filename)
			}
			configPath := filepath.Join(dir, "config.yaml")
			wantPath := ""
			if tc.filename != "" {
				wantPath = filepath.Join(dir, tc.filename)
			}
			if got := tc.source.LockPath(configPath); got != wantPath {
				t.Errorf("LockPath(%q) = %q, want %q", configPath, got, wantPath)
			}
		})
	}
}

func assertLayerResults(t *testing.T, got []LayerResult, wantSources []Source, wantPaths []string) {
	t.Helper()
	if len(got) != len(wantSources) {
		t.Fatalf("layer count = %d, want %d", len(got), len(wantSources))
	}
	for i := range got {
		if got[i].Source != wantSources[i] {
			t.Errorf("layer %d source = %v, want %v", i, got[i].Source, wantSources[i])
		}
		if got[i].Path != wantPaths[i] {
			t.Errorf("layer %d path = %q, want %q", i, got[i].Path, wantPaths[i])
		}
	}
}
