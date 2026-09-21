package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestResolveBlockedRegistryRemovalDoesNotAcquire(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "effective files", true: "custom loaders"}[custom], func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			blocked := []string{}
			cfg := &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}, AllowedRegistries: &blocked, Registries: []config.RegistryEntry{{URL: "https://example.invalid/registry.git", Ref: "main", Skills: config.KindSelector{Include: []string{"keeper"}}}}}
			opts := ResolveOptions{Workspace: mustWorkspace(t, repo), CatalogMode: catalog.LoadModeRefresh, IgnoreBlockedRegistries: true}
			if custom {
				opts.Loaders = Loaders{Repo: func() (*config.Config, error) { return cfg, nil }}
				opts.EffectiveOpts.RepoPath = filepath.Join(repo, ".agents", "config.yaml")
			} else {
				path := config.ProjectConfigPath(repo)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nallowed_registries: []\nregistries:\n  - url: https://example.invalid/registry.git\n    ref: main\n    skills: {include: [keeper]}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var clones atomic.Int32
			opts.CatalogOption = []catalog.LoadOption{catalog.WithCloneOrPull(func(string, string) (string, error) {
				clones.Add(1)
				return "", errors.New("unexpected clone")
			})}
			result := Resolve(context.Background(), opts)
			if clones.Load() != 0 {
				t.Fatalf("clone calls = %d", clones.Load())
			}
			if err := errors.Join(result.Errors...); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("blocked registry removal error = %v, want os.ErrNotExist", err)
			}
		})
	}
}

func TestResolveBlockedRegistryAllowedPolicyStillAcquires(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	allowed := []string{`example\.invalid`}
	cfg := &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}, AllowedRegistries: &allowed, Registries: []config.RegistryEntry{{URL: "https://example.invalid/registry.git", Ref: "main", Skills: config.KindSelector{Include: []string{"keeper"}}}}}
	var clones atomic.Int32
	result := Resolve(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo), CatalogMode: catalog.LoadModeRefresh, IgnoreBlockedRegistries: true, Loaders: Loaders{Repo: func() (*config.Config, error) { return cfg, nil }}, EffectiveOpts: config.LoadEffectiveOptions{RepoPath: filepath.Join(repo, ".agents", "config.yaml")}, CatalogOption: []catalog.LoadOption{catalog.WithCloneOrPull(func(string, string) (string, error) {
		clones.Add(1)
		return "", errors.New("expected fixture acquisition")
	})}})
	if clones.Load() != 1 || len(result.Errors) == 0 {
		t.Fatalf("allowed resolve clones=%d errors=%v", clones.Load(), result.Errors)
	}
}

func TestResolveBlockedRegistryDoesNotFilterMalformedConfig(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	path := config.ProjectConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	result := Resolve(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo), CatalogMode: catalog.LoadModeRefresh, IgnoreBlockedRegistries: true})
	if len(result.Errors) == 0 {
		t.Fatal("malformed config error was filtered")
	}
}
