package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// BenchmarkValidateHarnessTargets measures the target validation performed on
// every loaded config layer.
func BenchmarkValidateHarnessTargets(b *testing.B) {
	compilerNames := targetmeta.CompilerNames()
	ingesterNames := targetmeta.IngesterNames()
	if len(compilerNames) == 0 || len(ingesterNames) == 0 {
		b.Fatal("target registry must declare compiler and ingester targets")
	}

	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  append([]string(nil), compilerNames...),
		Registries: make([]RegistryEntry, 20),
	}
	for i := range cfg.Registries {
		cfg.Registries[i] = RegistryEntry{
			URL:           fmt.Sprintf("git@example.com:registry-%d.git", i),
			Ref:           "main",
			Harnesses:     append([]string(nil), compilerNames...),
			SourceHarness: ingesterNames[i%len(ingesterNames)],
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cfg.ValidateWithoutHarnesses(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMergeLayersLargeConfig measures the cost of merging many layers
// each with many registry entries.
func BenchmarkMergeLayersLargeConfig(b *testing.B) {
	cases := []struct {
		name      string
		numLayers int
		perLayer  int
	}{
		{"3layers_20entries", 3, 20},
		{"5layers_50entries", 5, 50},
		{"10layers_100entries", 10, 100},
	}

	sources := []Source{Organization, UserPersonal, RepoCommitted, RepoPersonal}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			layers := make([]Layer, c.numLayers)
			for l := 0; l < c.numLayers; l++ {
				cfg := &Config{
					APIVersion: APIVersionV4,
					Harnesses:  []string{"factory", "cursor"},
				}
				for e := 0; e < c.perLayer; e++ {
					cfg.Registries = append(cfg.Registries, RegistryEntry{
						URL:    fmt.Sprintf("git@example.com:layer%d-reg%02d.git", l, e),
						Ref:    "main",
						Skills: KindSelector{Include: []string{fmt.Sprintf("skill-%d", e%10)}},
						Tags:   []string{fmt.Sprintf("tag-%d", l%3)},
					})
				}
				layers[l] = Layer{Source: sources[l%len(sources)], Config: cfg}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = MergeLayers(layers)
			}
		})
	}
}

// BenchmarkLoadConfigFromPathLargeYAML measures the cost of loading and
// validating a large config file from disk.
func BenchmarkLoadConfigFromPathLargeYAML(b *testing.B) {
	cases := []struct {
		name    string
		entries int
	}{
		{"20_entries", 20},
		{"100_entries", 100},
		{"500_entries", 500},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			dir := b.TempDir()
			configPath := filepath.Join(dir, "config.yaml")

			var yaml string
			yaml = "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n  - cursor\nsources:\n"
			for e := 0; e < c.entries; e++ {
				yaml += fmt.Sprintf("  reg-%03d: {git: \"git@example.com:reg-%03d.git\", ref: main}\n", e, e)
			}
			yaml += "resources:\n"
			for e := 0; e < c.entries; e++ {
				yaml += fmt.Sprintf("  reg-%03d:\n    include: [skill/skill-%03d]\n    tags: [tag-%d]\n", e, e, e%5)
			}
			if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cfg, err := LoadConfigFromPath(configPath)
				if err != nil || cfg == nil || len(cfg.Registries) != c.entries {
					b.Fatalf("load %d entries: config=%#v, err=%v", c.entries, cfg, err)
				}
			}
		})
	}
}

// BenchmarkValidateRegistryURLsLargePolicy measures the cost of validating
// many registry URLs against a policy pattern set.
func BenchmarkValidateRegistryURLsLargePolicy(b *testing.B) {
	cases := []struct {
		name    string
		entries int
	}{
		{"50_entries", 50},
		{"200_entries", 200},
		{"1000_entries", 1000},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			entries := make([]RegistryEntry, c.entries)
			for e := 0; e < c.entries; e++ {
				entries[e] = RegistryEntry{
					URL: fmt.Sprintf("https://gitlab.example.com/team-%d/repo.git", e),
					Ref: "main",
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = ValidateRegistryURLs(entries, DefaultAllowedRegistries)
			}
		})
	}
}

// BenchmarkFindRepoRoot measures a repeat FindRepoRoot call: the upward walk
// runs every time, and the git boundary is served from the per-directory memo.
// This is the shape every caller after the first one sees, and commands make
// between 3 and 7 of these calls.
func BenchmarkFindRepoRoot(b *testing.B) {
	chdirToRepoRootFixture(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FindRepoRoot()
	}
}

// BenchmarkFindRepoRootCold measures the first FindRepoRoot call of a process:
// the memo is empty, so this still pays one `git rev-parse` fork. It is the
// regression guard for the un-memoized path — if a future change reintroduces
// a fork per call, BenchmarkFindRepoRoot converges on this number.
func BenchmarkFindRepoRootCold(b *testing.B) {
	chdirToRepoRootFixture(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ResetRepoRootCache()
		_ = FindRepoRoot()
	}
}

// chdirToRepoRootFixture builds a project tree with a .agents/config.yaml at
// its root, makes a nested subdirectory the working directory for the duration
// of the benchmark, and clears the git-root memo on both ends so neither
// benchmark inherits or leaks a cached answer.
func chdirToRepoRootFixture(b *testing.B) {
	b.Helper()

	dir := b.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	subDir := filepath.Join(dir, "src", "internal", "resolver")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		b.Fatal(err)
	}
	b.Chdir(subDir)
	ResetRepoRootCache()

	b.Cleanup(func() {
		ResetRepoRootCache()
	})
}

// BenchmarkEditorAddRemoveLargeYAML measures v4 source-bound exclusion and
// include removal on large YAML files. Timed work includes the editor's disk
// reads and candidate validation, but excludes fixture resets and publication;
// no network requests occur.
func BenchmarkEditorAddRemoveLargeYAML(b *testing.B) {
	cases := []struct {
		name    string
		entries int
	}{
		{"50_entries", 50},
		{"200_entries", 200},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			var yaml string
			yaml = "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n  - cursor\nsources:\n"
			for e := 0; e < c.entries; e++ {
				yaml += fmt.Sprintf("  reg-%03d: {git: \"git@example.com:reg-%03d.git\", ref: main}\n", e, e)
			}
			yaml += "resources:\n"
			for e := 0; e < c.entries; e++ {
				yaml += fmt.Sprintf("  reg-%03d: [skill/skill-%03d]\n", e, e)
			}
			baseYAML := []byte(yaml)
			configPath := filepath.Join(b.TempDir(), "config.yaml")

			target := LayerTarget{Source: RepoCommitted, StagedPath: configPath, DeclaringPath: configPath}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				if err := os.WriteFile(configPath, baseYAML, 0o644); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				editor, err := OpenLayerEditor(target)
				if err != nil {
					b.Fatal(err)
				}
				for index, operation := range []SelectionOperation{SelectionExclude, SelectionRemoveInclude} {
					binding, found, err := editor.LookupRegistry(RegistryEntry{URL: fmt.Sprintf("git@example.com:reg-%03d.git", index), Ref: "main"})
					if err != nil || !found {
						b.Fatalf("lookup existing source %d: found=%v, err=%v", index, found, err)
					}
					if err := editor.Apply(binding, SelectionChange{Operation: operation, Locators: []ResourceLocator{{Kind: ir.KindSkill, Name: fmt.Sprintf("skill-%03d", index)}}}); err != nil {
						b.Fatal(err)
					}
				}
				candidate, err := editor.Render()
				if err != nil || !candidate.Changed {
					b.Fatalf("render edited config: changed=%v, err=%v", candidate.Changed, err)
				}
			}
		})
	}
}
