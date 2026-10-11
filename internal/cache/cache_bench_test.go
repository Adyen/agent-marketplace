package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkBaseCachePath measures the suffix-stripping hot path used by
// agent-marketplace status.
func BenchmarkBaseCachePath(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	base := RegistryCachePath("git@example.com:repo.git", "main")
	keyWithSuffix := base + ":" + HashURL("skills")
	keyNoSuffix := base

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BaseCachePath(keyWithSuffix)
		_ = BaseCachePath(keyNoSuffix)
	}
}

// BenchmarkCatalogKeyForEntry measures catalog key construction.
func BenchmarkCatalogKeyForEntry(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CatalogKeyForEntry("git@example.com:repo.git", "main", "skills")
		_ = CatalogKeyForEntry("git@example.com:repo.git", "main", "")
	}
}

// BenchmarkActiveRegistryHashes_ManyRegistries measures activeRegistryHashes
// with many registries across layers.
func BenchmarkActiveRegistryHashes_ManyRegistries(b *testing.B) {
	cases := []struct {
		name      string
		numLayers int
		perLayer  int
	}{
		{"3layers_50regs", 3, 50},
		{"5layers_100regs", 5, 100},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			var sources []string
			for l := 0; l < c.numLayers; l++ {
				for r := 0; r < c.perLayer; r++ {
					sources = append(sources, fmt.Sprintf("git@example.com:layer%d-reg%03d.git", l, r))
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = activeRegistryHashes(sources)
			}
		})
	}
}

// BenchmarkGarbageCollectDryRun measures the GC dry-run cost with many cache
// directories.
func BenchmarkGarbageCollectDryRun(b *testing.B) {
	cases := []struct {
		name      string
		numDirs   int
		numActive int
	}{
		{"10_dirs_3_active", 10, 3},
		{"50_dirs_10_active", 50, 10},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			cacheDir := b.TempDir()
			oldHome := os.Getenv("HOME")
			b.Setenv("HOME", cacheDir)
			b.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

			cacheDir = filepath.Join(cacheDir, ".cache", "agent-marketplace")
			if err := os.MkdirAll(cacheDir, 0o755); err != nil {
				b.Fatal(err)
			}

			var sources []string
			for a := 0; a < c.numActive; a++ {
				url := fmt.Sprintf("git@example.com:repo-%d.git", a)
				sources = append(sources, url)
				if err := os.MkdirAll(filepath.Join(cacheDir, HashURL(url)), 0o755); err != nil {
					b.Fatal(err)
				}
			}
			for d := c.numActive; d < c.numDirs; d++ {
				hash := fmt.Sprintf("orphan-%09d", d)
				if err := os.MkdirAll(filepath.Join(cacheDir, hash), 0o755); err != nil {
					b.Fatal(err)
				}
			}
			reader := staticReachability(nil, sources...)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := GarbageCollect(context.Background(), true, reader); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCloneOrPullWarmLocalRepo measures the warm-cache clone-or-pull
// cost with a local bare repository.
func BenchmarkCloneOrPullWarmLocalRepo(b *testing.B) {
	remoteDir := b.TempDir()
	if err := runGit("", "init", "--bare", remoteDir); err != nil {
		b.Fatal(err)
	}
	for _, setting := range []string{"receive.autogc", "maintenance.auto"} {
		if err := runGit(remoteDir, "config", setting, "false"); err != nil {
			b.Fatal(err)
		}
	}
	workDir := b.TempDir()
	if err := runGit("", "clone", remoteDir, workDir); err != nil {
		b.Fatal(err)
	}
	if err := runGit(workDir, "checkout", "-b", "main"); err != nil {
		b.Fatal(err)
	}
	if err := runGit(workDir,
		"-c", "commit.gpgsign=false",
		"-c", "user.name=Benchmark",
		"-c", "user.email=benchmark@example.com",
		"commit", "--allow-empty", "-m", "init",
	); err != nil {
		b.Fatal(err)
	}
	if err := runGit(workDir, "push", "origin", "main"); err != nil {
		b.Fatal(err)
	}

	url := "file://" + remoteDir
	b.Setenv("HOME", b.TempDir())
	if _, err := CloneOrPull(url, "main"); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CloneOrPull(url, "main"); err != nil {
			b.Fatal(err)
		}
	}
}
