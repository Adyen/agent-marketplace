package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func BenchmarkTransientOrganizationMarketplaceLockAfterSelectionEdit(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(b, func() { requests.Add(1) })
	defer server.Close()

	authority := b.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	orgConfig := []byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: ` + server.URL + `}
resources:
  hosted: {include: [tools/skill/review]}
`)
	if err := os.WriteFile(orgPath, orgConfig, 0o644); err != nil {
		b.Fatal(err)
	}
	userPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(b, orgPath)
	repo := b.TempDir()
	opts := pipeline.ResolveOptions{
		RepoRoot: repo,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         filepath.Join(repo, ".agents", "config.yaml"),
			LocalPath:        filepath.Join(repo, ".agents", "config.local.yaml"),
		},
		Loaders: pipeline.Loaders{
			Org:    config.LoadOrganizationConfig,
			Global: func() (*config.Config, error) { return config.LoadConfigFromPath(userPath) },
			Repo:   func() (*config.Config, error) { return nil, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	}
	ctx := pipeline.WithTransientOrganizationMarketplaceLockCache(context.Background())
	if result := pipeline.Resolve(ctx, opts); errors.Join(result.Errors...) != nil {
		b.Fatalf("prime resolve: %v", errors.Join(result.Errors...))
	}

	b.ResetTimer()
	startRequests := requests.Load()
	for i := 0; i < b.N; i++ {
		data := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n# iteration " + strconv.Itoa(i) + "\n")
		if err := os.WriteFile(userPath, data, 0o644); err != nil {
			b.Fatalf("rewrite user config: %v", err)
		}
		result := pipeline.Resolve(ctx, opts)
		if err := errors.Join(result.Errors...); err != nil {
			b.Fatalf("resolve iteration %d: %v", i, err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(requests.Load()-startRequests)/float64(b.N), "requests/op")
}
