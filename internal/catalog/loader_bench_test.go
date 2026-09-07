package catalog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// benchSubBundleCatalog returns a LoadedCatalog with nSkills entries.
func benchSubBundleCatalog(nSkills int) LoadedCatalog {
	entries := make([]Entry, 0, nSkills)
	for i := 0; i < nSkills; i++ {
		entries = append(entries, Entry{
			Kind: ir.KindSkill,
			Name: fmt.Sprintf("skill-%03d", i),
			Path: fmt.Sprintf("/fake/%03d/skills/skill-%03d", i, i),
			Tags: []string{"bench"},
		})
	}
	resources := make(map[string]ir.Resource, nSkills)
	for i := 0; i < nSkills; i++ {
		key := skill.ResourceKey(ir.KindSkill, fmt.Sprintf("skill-%03d", i))
		resources[key] = &benchFakeResource{kind: ir.KindSkill, name: fmt.Sprintf("skill-%03d", i)}
	}
	return LoadedCatalog{Entries: entries, Resources: resources}
}

type benchFakeResource struct {
	kind ir.ResourceKind
	name string
}

func (r *benchFakeResource) Kind() ir.ResourceKind { return r.kind }
func (r *benchFakeResource) Name() string          { return r.name }

// BenchmarkDiscoverAll_SubBundles benchmarks discoverAll through the branch
// where len(discovered.SubBundles) > 0, exercising processSubBundles and
// the code paths that populate result.Catalogs and discoveredByKey for
// the parent entry's catalog key.
func BenchmarkDiscoverAll_SubBundles(b *testing.B) {
	const nSubBundles = 5
	const nSkills = 20

	regDir := b.TempDir()
	cloneOrPull := func(url, ref string) (string, error) { return regDir, nil }

	// Build a discover func that returns SubBundles for the root dir
	// and plain catalogs for sub-bundle dirs.
	subBundleDiscover := func(dir, sourceHarness string) (LoadedCatalog, error) {
		if dir == regDir {
			subBundles := make(map[string]LoadedCatalog, nSubBundles)
			mergedEntries := make([]Entry, 0, nSubBundles*nSkills)
			mergedResources := make(map[string]ir.Resource, nSubBundles*nSkills)
			seen := make(map[string]struct{})
			for i := 0; i < nSubBundles; i++ {
				subDir := fmt.Sprintf("%s/plugin-%03d", regDir, i)
				cat := benchSubBundleCatalog(nSkills)
				subBundles[subDir] = cat
				for _, e := range cat.Entries {
					key := skill.ResourceKey(e.Kind, e.Name)
					if _, exists := seen[key]; exists {
						continue
					}
					seen[key] = struct{}{}
					mergedEntries = append(mergedEntries, e)
				}
				for k, v := range cat.Resources {
					if _, exists := mergedResources[k]; !exists {
						mergedResources[k] = v
					}
				}
			}
			return LoadedCatalog{
				Entries:    mergedEntries,
				Resources:  mergedResources,
				SubBundles: subBundles,
			}, nil
		}
		return benchSubBundleCatalog(nSkills), nil
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:           regDir,
				Ref:           "",
				Source:        config.RepoCommitted,
				SourceHarness: "claude",
			}},
		}
		result := LoadResult{
			Config:           &config.Config{},
			Catalogs:         make(map[string]LoadedCatalog),
			ResourceCatalogs: make(map[string]map[string]ir.Resource),
		}
		requests, jobs, _ := prepareFetchJobs(cfg, "", LoadModeRefresh, "")
		runFetches(jobs, LoadModeRefresh, cloneOrPull)
		discoverAll(&result, requests, subBundleDiscover)
	}
}

// BenchmarkDiscoverAll_NormalPath benchmarks discoverAll through the normal
// (non-sub-bundle) path for comparison.
func BenchmarkDiscoverAll_NormalPath(b *testing.B) {
	const nSkills = 100

	regDir := b.TempDir()
	cloneOrPull := func(url, ref string) (string, error) { return regDir, nil }
	normalDiscover := func(dir, sourceHarness string) (LoadedCatalog, error) {
		return benchSubBundleCatalog(nSkills), nil
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{{
				URL:           regDir,
				Ref:           "",
				Source:        config.RepoCommitted,
				SourceHarness: "",
			}},
		}
		result := LoadResult{
			Config:           &config.Config{},
			Catalogs:         make(map[string]LoadedCatalog),
			ResourceCatalogs: make(map[string]map[string]ir.Resource),
		}
		requests, jobs, _ := prepareFetchJobs(cfg, "", LoadModeRefresh, "")
		runFetches(jobs, LoadModeRefresh, cloneOrPull)
		discoverAll(&result, requests, normalDiscover)
	}
}

// BenchmarkHasBundleRoot measures the performance of hasBundleRoot, which
// sits on the recursive discovery path and calls ingest.AutodetectStrict
// on every directory during scanSubBundlesWalk.
func BenchmarkHasBundleRoot(b *testing.B) {
	b.Run("harness_root", func(b *testing.B) {
		dir := b.TempDir()
		skillDir := filepath.Join(dir, "skills", "review")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\n---\n"), 0o644); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			hasBundleRoot(dir)
		}
	})
	b.Run("multi_skill_subdirs", func(b *testing.B) {
		dir := b.TempDir()
		for i := 0; i < 10; i++ {
			skillDir := filepath.Join(dir, fmt.Sprintf("skill-%03d", i))
			if err := os.MkdirAll(skillDir, 0o755); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(fmt.Sprintf("---\nname: skill-%03d\n---\n", i)), 0o644); err != nil {
				b.Fatal(err)
			}
		}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			hasBundleRoot(dir)
		}
	})
	b.Run("non_bundle_dir", func(b *testing.B) {
		dir := b.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello"), 0o644); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			hasBundleRoot(dir)
		}
	})
}

func BenchmarkDiscoverSubBundleEvidence(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
	for _, rootResources := range []bool{false, true} {
		b.Run(fmt.Sprintf("root_resources_%v", rootResources), func(b *testing.B) {
			dir := b.TempDir()
			files := map[string]string{
				".github/agents/review.md":    "Repository automation",
				"automation/agents/runner.sh": "exit 0",
				"automation/hooks/pre-commit": "exit 0",
			}
			for i := 0; i < 10; i++ {
				files[fmt.Sprintf("plugin-%02d/commands/review.md", i)] = fmt.Sprintf("---\nname: review-%02d\ndescription: Review\n---\nReview", i)
			}
			if rootResources {
				files["commands/root.md"] = "---\nname: root\ndescription: Root\n---\nRoot"
			}
			writeDiscoveryFiles(b, dir, files)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Discover(dir, "claude"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPrepareFetchJobs_LocalRegistry exercises the local-registry path
// in prepareFetchJobs, including the stat check added to fail fast on missing
// local directories.
func BenchmarkPrepareFetchJobs_LocalRegistry(b *testing.B) {
	regDir := b.TempDir()
	skillDir := filepath.Join(regDir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\n---\n"), 0o644); err != nil {
		b.Fatal(err)
	}

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    regDir,
			Source: config.RepoCommitted,
		}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, errs := prepareFetchJobs(cfg, "", LoadModeRefresh, "")
		if len(errs) > 0 {
			b.Fatalf("unexpected errors: %v", errs)
		}
	}
}
