package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"

	"github.com/Adyen/agent-marketplace/internal/registryfs"
)

// BenchmarkLoadFSHCCIngest measures the cost of HCC-based catalog loading
// from an in-memory filesystem with synthetic skill directories.
func BenchmarkLoadFSHCCIngest(b *testing.B) {
	cases := []int{10, 50, 200}

	for _, n := range cases {
		b.Run(fmt.Sprintf("%d_skills", n), func(b *testing.B) {
			fsys := make(fstest.MapFS)
			for i := 0; i < n; i++ {
				name := fmt.Sprintf("skill-%04d", i)
				dir := fmt.Sprintf("skills/%s", name)
				fsys[dir+"/SKILL.md"] = &fstest.MapFile{
					Data: []byte(fmt.Sprintf("---\nname: %s\ndescription: skill %d\n---\n\n# %s\n\nBody text\n", name, i, name)),
				}
			}
			fsys[".claude-plugin/plugin.json"] = &fstest.MapFile{
				Data: []byte(`{"name": "test-plugin", "version": "1.0.0"}`),
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = LoadFS(fsys, "")
			}
		})
	}
}

// BenchmarkLoadRegistriesMockedDiscover measures LoadRegistries with a
// mocked discover function that returns synthetic catalogs without git.
func BenchmarkLoadRegistriesMockedDiscover(b *testing.B) {
	cases := []struct {
		name       string
		registries int
		perReg     int
	}{
		{"5reg_50skills", 5, 50},
		{"10reg_100skills", 10, 100},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.Setenv("HOME", b.TempDir())

			cfg := &config.Config{
				Harnesses: []string{"factory"},
			}
			for r := 0; r < c.registries; r++ {
				url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
				cachePath := cache.RegistryCachePath(url, "main")
				if err := os.MkdirAll(cachePath, 0o755); err != nil {
					b.Fatal(err)
				}
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{
					URL:    url,
					Ref:    "main",
					Source: config.RepoCommitted,
				})
			}

			mockDiscover := func(source registryfs.Policy, _ string) (LoadedCatalog, error) {
				entries := make([]Entry, c.perReg)
				for s := 0; s < c.perReg; s++ {
					entries[s] = Entry{
						Kind: ir.KindSkill,
						Name: fmt.Sprintf("skill-%04d", s),
						Path: filepath.Join(source.Root, fmt.Sprintf("skill-%04d", s), "SKILL.md"),
					}
				}
				return LoadedCatalog{Entries: entries}, nil
			}

			repoRoot := b.TempDir()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result := LoadRegistries(cfg, repoRoot, LoadModeInspect,
					WithDiscoverFunc(mockDiscover),
					WithCloneOrPull(func(url, ref string) (string, error) {
						return cache.RegistryCachePath(url, ref), nil
					}),
				)
				if len(result.Errors) > 0 {
					b.Fatal(result.Errors)
				}
			}
		})
	}
}

// BenchmarkDiscoverLegacyFallback measures the Discover fallback path that
// uses skill.DiscoverSkills when HCC ingestion fails.
func BenchmarkDiscoverLegacyFallback(b *testing.B) {
	cases := []int{20, 100, 500}

	for _, n := range cases {
		b.Run(fmt.Sprintf("%d_skills", n), func(b *testing.B) {
			registryDir := b.TempDir()
			for i := 0; i < n; i++ {
				dir := filepath.Join(registryDir, fmt.Sprintf("skill-%04d", i))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					b.Fatal(err)
				}
				content := fmt.Sprintf(`---
name: skill-%04d
description: Synthetic skill for benchmarking
metadata:
  tags:
    - tag-%d
    - tag-common
---

# Skill %d

%s
`, i, i%7, i, strings.Repeat("lorem ipsum dolor sit amet.\n", 20))
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
					b.Fatal(err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Discover(registryfs.Local(registryDir), "claude")
			}
		})
	}
}
