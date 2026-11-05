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
			cfg := &config.Config{APIVersion: config.APIVersionV4, Harnesses: []string{"factory"}, AllowedRegistries: &blocked, Registries: []config.RegistryEntry{{URL: "https://example.invalid/registry.git", Ref: "main", Skills: config.KindSelector{Include: []string{"keeper"}}}}}
			opts := ResolveOptions{Workspace: mustWorkspace(t, repo), CatalogMode: catalog.LoadModeRefresh, IgnoreBlockedRegistries: true}
			if custom {
				opts.Loaders = Loaders{Repo: func() (*config.Config, error) { return cfg, nil }}
				opts.EffectiveOpts.RepoPath = filepath.Join(repo, ".agents", "config.yaml")
			} else {
				path := config.ProjectConfigPath(repo)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_registries: []\nsources:\n  registry: {git: https://example.invalid/registry.git, ref: main}\nresources:\n  registry: [skill/keeper]\n"), 0o600); err != nil {
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
			if len(result.ConfigErrors) != 0 || result.MergedCfg == nil || len(result.MergedCfg.Registries) != 1 {
				t.Fatalf("blocked removal did not reach the configured registry: merged=%#v config errors=%v", result.MergedCfg, result.ConfigErrors)
			}
			entry := result.MergedCfg.Registries[0]
			if entry.URL != cfg.Registries[0].URL || entry.Ref != "main" || len(entry.Skills.Include) != 1 || entry.Skills.Include[0] != "keeper" {
				t.Fatalf("blocked removal lost the configured registry selection: %#v", entry)
			}
			if err := errors.Join(result.Errors...); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("blocked registry removal error = %v, want os.ErrNotExist", err)
			}
		})
	}
}

func TestResolveBlockedRegistryAllowedPolicyStillAcquires(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "effective files", true: "custom loaders"}[custom], func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			allowed := []string{`example\.invalid`}
			cfg := &config.Config{APIVersion: config.APIVersionV4, Harnesses: []string{"factory"}, AllowedRegistries: &allowed, Registries: []config.RegistryEntry{{URL: "https://example.invalid/registry.git", Ref: "main", Skills: config.KindSelector{Include: []string{"keeper"}}}}}
			opts := ResolveOptions{Workspace: mustWorkspace(t, repo), CatalogMode: catalog.LoadModeRefresh, IgnoreBlockedRegistries: true}
			if custom {
				opts.Loaders = Loaders{Repo: func() (*config.Config, error) { return cfg, nil }}
				opts.EffectiveOpts.RepoPath = config.ProjectConfigPath(repo)
			} else {
				writeFile(t, config.ProjectConfigPath(repo), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_registries: ['example\\.invalid']\nsources:\n  registry: {git: https://example.invalid/registry.git, ref: main}\nresources:\n  registry: [skill/keeper]\n")
			}
			var clones atomic.Int32
			acquisitionErr := errors.New("expected fixture acquisition")
			opts.CatalogOption = []catalog.LoadOption{catalog.WithCloneOrPull(func(string, string) (string, error) {
				clones.Add(1)
				return "", acquisitionErr
			})}
			result := Resolve(context.Background(), opts)
			if clones.Load() != 1 || !errors.Is(errors.Join(result.Errors...), acquisitionErr) {
				t.Fatalf("allowed resolve clones=%d errors=%v, want one fixture acquisition", clones.Load(), result.Errors)
			}
		})
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
	var validation *config.V4ValidationError
	if !errors.As(errors.Join(result.Errors...), &validation) || validation.Code != "v4.parse" {
		t.Fatalf("malformed config error = %v, want unfiltered v4.parse", result.Errors)
	}
}
