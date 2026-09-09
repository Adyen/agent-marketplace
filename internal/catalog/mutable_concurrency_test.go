package catalog

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestMutableDiscoveryProtectsCheckoutAcrossRefs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, output)
		}
	}
	origin := filepath.Join(t.TempDir(), "registry.git")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(origin, "init", "--initial-branch=main")
	writeFile(t, filepath.Join(origin, "content"), "before")
	runGit(origin, "add", ".")
	runGit(origin, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "seed")
	runGit(origin, "branch", "feature/nested")
	url := "file://" + origin
	if _, err := cache.CloneOrPull(url, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(origin, "content"), "after")
	runGit(origin, "add", ".")
	runGit(origin, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "advance")
	for _, ref := range []string{"main", "feature/nested"} {
		t.Run(ref, func(t *testing.T) {
			cfg := &config.Config{Registries: []config.RegistryEntry{{URL: url, Ref: "main"}}}
			writerDone := make(chan error, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := LoadRegistriesWithContext(ctx, cfg, "", LoadModeCachedNoFetch, WithDiscoverFunc(func(dir, _ string) (LoadedCatalog, error) {
				if dir != cache.RegistryCachePath(url, "main") {
					return LoadedCatalog{}, fmt.Errorf("discovery bypassed mutable cache: %s", dir)
				}
				before, err := os.ReadFile(filepath.Join(dir, "content"))
				if err != nil {
					return LoadedCatalog{}, err
				}
				go func() { _, err := cache.CloneOrPull(url, ref); writerDone <- err }()
				select {
				case err := <-writerDone:
					writerDone <- err
					return LoadedCatalog{}, fmt.Errorf("checkout completed during discovery: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				independentDone := make(chan error, 1)
				go func() {
					_, err := cache.CloneOrPull("file://"+filepath.Join(origin, ".git"), "main")
					independentDone <- err
				}()
				select {
				case err := <-independentDone:
					if err != nil {
						return LoadedCatalog{}, err
					}
				case <-ctx.Done():
					return LoadedCatalog{}, ctx.Err()
				}
				after, err := os.ReadFile(filepath.Join(dir, "content"))
				if err != nil || string(after) != string(before) {
					return LoadedCatalog{}, fmt.Errorf("discovery bytes changed: %q to %q: %v", before, after, err)
				}
				return LoadedCatalog{}, nil
			}))
			if len(result.Errors) != 0 {
				t.Error(result.Errors)
			}
			select {
			case err := <-writerDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("writer did not resume after discovery")
			}
			content, err := os.ReadFile(filepath.Join(cache.RegistryCachePath(url, ref), "content"))
			want := "after"
			if ref != "main" {
				want = "before"
			}
			if err != nil || string(content) != want {
				t.Fatalf("checkout result: %q, %v", content, err)
			}
		})
	}
}
