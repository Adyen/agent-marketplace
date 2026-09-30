package resolver

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkResolve captures the cost of merging registry catalogs through
// the full resolver pipeline: selection, dedup, cross-layer excludes,
// local-skill application, personal excludes. Parameterised over the number
// of registries and skills-per-registry so regressions on either axis are
// visible.
func BenchmarkResolve(b *testing.B) {
	cases := []struct {
		registries int
		perReg     int
	}{
		{5, 20},
		{10, 50},
		{20, 100},
	}

	for _, c := range cases {
		name := fmt.Sprintf("%dregs_%dskills", c.registries, c.perReg)
		b.Run(name, func(b *testing.B) {
			cfg := &config.Config{}
			catalogs := map[string][]catalog.Entry{}
			for r := 0; r < c.registries; r++ {
				url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
				ref := "main"
				entry := config.RegistryEntry{
					URL:    url,
					Ref:    ref,
					Tags:   []string{fmt.Sprintf("tag-%d", r%5)},
					Source: config.RepoCommitted,
				}
				cfg.Registries = append(cfg.Registries, entry)

				skills := make([]catalog.Entry, 0, c.perReg)
				for s := 0; s < c.perReg; s++ {
					skills = append(skills, catalog.Entry{
						Name: fmt.Sprintf("reg%02d-skill-%03d", r, s),
						Tags: []string{fmt.Sprintf("tag-%d", (r+s)%5), "common"},
						Path: fmt.Sprintf("/cache/reg%02d/skill-%03d", r, s),
					})
				}
				catalogs[cache.CatalogKeyForEntry(url, ref, "")] = skills
			}
			setCatalogKeys(cfg)

			localCfg := &config.LocalConfig{
				Skills: config.KindSelector{
					Include: []string{"reg00-skill-000", "reg01-skill-000"},
					Exclude: []string{"reg02-skill-010"},
				},
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Resolve(cfg, catalogs, localCfg)
			}
		})
	}
}

func BenchmarkResolveCrossScopeCollisions(b *testing.B) {
	const skills = 50
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.com:user.git", Ref: "main", Source: config.UserPersonal, Default: true},
		{URL: "git@example.com:organization.git", Ref: "main", Source: config.Organization, Default: true, Required: true},
		{URL: "git@example.com:repository.git", Ref: "main", Source: config.RepoCommitted, Default: true},
	}}
	setCatalogKeys(cfg)
	catalogs := make(map[string][]catalog.Entry, len(cfg.Registries))
	for _, source := range cfg.Registries {
		entries := make([]catalog.Entry, skills)
		for i := range entries {
			entries[i] = catalog.Entry{Kind: ir.KindSkill, Name: fmt.Sprintf("skill-%02d", i)}
		}
		catalogs[source.CatalogKey] = entries
	}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Resolve(cfg, catalogs, nil)
	}
}

// BenchmarkResolveCrossScopeCollectionMembers covers required and optional
// collection members whose installed names repository sources also select,
// including the reselection a required collection member triggers when a
// direct repository selection would shadow it.
func BenchmarkResolveCrossScopeCollectionMembers(b *testing.B) {
	const skills = 50
	members := make([]ir.CollectionMember, skills)
	names := make([]string, skills)
	for i := range members {
		names[i] = fmt.Sprintf("skill-%02d", i)
		members[i] = ir.CollectionMember{Name: names[i], Kind: ir.KindSkill}
	}
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.com:organization.git", Ref: "main", Source: config.Organization, Required: true, Collections: ks("bundle-organization")},
		{URL: "git@example.com:user.git", Ref: "main", Source: config.UserPersonal, Collections: ks("bundle-user")},
		{URL: "git@example.com:organization.git", Ref: "main", Source: config.RepoCommitted, Skills: ks(names...)},
		{URL: "git@example.com:another.git", Ref: "main", Source: config.RepoCommitted, Skills: ks(names...)},
	}}
	setCatalogKeys(cfg)
	catalogs := make(map[string][]catalog.Entry, len(cfg.Registries))
	for _, source := range cfg.Registries {
		name := sharedCollectionName(source.URL)
		entries := make([]catalog.Entry, 0, skills+1)
		for _, skillName := range names {
			entries = append(entries, catalog.Entry{Kind: ir.KindSkill, Name: skillName, Path: "/cache/" + source.URL + "/" + skillName})
		}
		collection := ir.NewCollection(name, "", "", members, nil, nil, ir.SourceRef{})
		catalogs[source.CatalogKey] = append(entries, catalog.Entry{Kind: ir.KindCollection, Name: name, Resource: collection})
	}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Resolve(cfg, catalogs, nil)
	}
}

func BenchmarkResolveRequirements(b *testing.B) {
	const skillCount = 100
	url := "git@example.com:requirements.git"
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:     url,
		Ref:     "main",
		Default: true,
		Source:  config.RepoCommitted,
	}}}
	entries := make([]catalog.Entry, 0, skillCount+1)
	for i := 0; i < skillCount; i++ {
		name := fmt.Sprintf("required-skill-%03d", i)
		entries = append(entries, catalog.Entry{
			Kind: ir.KindSkill,
			Name: name,
			Resource: ir.NewSkill(name, "", "", nil, map[string]any{
				"metadata": map[string]any{
					"requires": []any{"mcp:genai-atlassian"},
				},
			}, ir.SourceRef{}),
		})
	}
	entries = append(entries, catalog.Entry{
		Kind:     ir.KindMcpServer,
		Name:     "genai-atlassian",
		Resource: ir.NewMcpServer("genai-atlassian", "", nil, nil, nil, ir.SourceRef{}),
	})
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cache.CatalogKeyForEntry(url, "main", ""): entries,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Resolve(cfg, catalogs, nil)
	}
}

// BenchmarkResolveDefaultAllLargeCatalog covers default:true and required:true
// all-resource selection at multiple registry/resource sizes.
func BenchmarkResolveDefaultAllLargeCatalog(b *testing.B) {
	cases := []struct {
		name       string
		registries int
		perReg     int
		required   bool
		default_   bool
	}{
		{"5reg_100skills_required", 5, 100, true, false},
		{"5reg_100skills_default", 5, 100, false, true},
		{"10reg_200skills_required", 10, 200, true, false},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			cfg := &config.Config{}
			catalogs := map[string][]catalog.Entry{}
			for r := 0; r < c.registries; r++ {
				url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
				ref := "main"
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{
					URL:      url,
					Ref:      ref,
					Required: c.required,
					Default:  c.default_,
					Source:   config.UserPersonal,
				})
				entries := make([]catalog.Entry, 0, c.perReg)
				for s := 0; s < c.perReg; s++ {
					entries = append(entries, catalog.Entry{
						Kind: ir.KindSkill,
						Name: fmt.Sprintf("reg%02d-skill-%03d", r, s),
						Path: fmt.Sprintf("/cache/reg%02d/skill-%03d", r, s),
					})
				}
				catalogs[cache.CatalogKeyForEntry(url, ref, "")] = entries
			}
			setCatalogKeys(cfg)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Resolve(cfg, catalogs, nil)
			}
		})
	}
}

// BenchmarkResolveTagSelectionLargeCatalog covers many tags per resource and
// multiple configured wanted tags.
func BenchmarkResolveTagSelectionLargeCatalog(b *testing.B) {
	cases := []struct {
		name       string
		registries int
		perReg     int
		tagsPerRes int
		wantedTags int
	}{
		{"5reg_100skills_3tags", 5, 100, 3, 2},
		{"5reg_100skills_5tags", 5, 100, 5, 3},
		{"10reg_100skills_3tags", 10, 100, 3, 2},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			cfg := &config.Config{}
			catalogs := map[string][]catalog.Entry{}
			wanted := make([]string, c.wantedTags)
			for t := 0; t < c.wantedTags; t++ {
				wanted[t] = fmt.Sprintf("want-tag-%d", t)
			}
			for r := 0; r < c.registries; r++ {
				url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
				ref := "main"
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{
					URL:    url,
					Ref:    ref,
					Tags:   wanted,
					Source: config.RepoCommitted,
				})
				entries := make([]catalog.Entry, 0, c.perReg)
				for s := 0; s < c.perReg; s++ {
					tags := make([]string, c.tagsPerRes)
					for t := 0; t < c.tagsPerRes; t++ {
						tags[t] = fmt.Sprintf("want-tag-%d", (s+t)%c.wantedTags)
					}
					entries = append(entries, catalog.Entry{
						Kind: ir.KindSkill,
						Name: fmt.Sprintf("reg%02d-skill-%03d", r, s),
						Tags: tags,
						Path: fmt.Sprintf("/cache/reg%02d/skill-%03d", r, s),
					})
				}
				catalogs[cache.CatalogKeyForEntry(url, ref, "")] = entries
			}
			setCatalogKeys(cfg)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Resolve(cfg, catalogs, nil)
			}
		})
	}
}

// BenchmarkResolveCrossLayerExcludes covers many resolved resources and large
// higher-precedence exclude lists.
func BenchmarkResolveCrossLayerExcludes(b *testing.B) {
	cases := []struct {
		name         string
		registries   int
		perReg       int
		excludeRatio float64
	}{
		{"5reg_100skills_20pct_exclude", 5, 100, 0.2},
		{"5reg_100skills_50pct_exclude", 5, 100, 0.5},
		{"10reg_100skills_30pct_exclude", 10, 100, 0.3},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			cfg := &config.Config{}
			catalogs := map[string][]catalog.Entry{}
			for r := 0; r < c.registries; r++ {
				url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
				ref := "main"
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{
					URL:     url,
					Ref:     ref,
					Default: true,
					Source:  config.UserPersonal,
				})
				entries := make([]catalog.Entry, 0, c.perReg)
				for s := 0; s < c.perReg; s++ {
					entries = append(entries, catalog.Entry{
						Kind: ir.KindSkill,
						Name: fmt.Sprintf("reg%02d-skill-%03d", r, s),
						Path: fmt.Sprintf("/cache/reg%02d/skill-%03d", r, s),
					})
				}
				catalogs[cache.CatalogKeyForEntry(url, ref, "")] = entries
			}
			excludeCount := int(float64(c.registries*c.perReg) * c.excludeRatio)
			excludes := make([]string, excludeCount)
			for e := 0; e < excludeCount; e++ {
				regIdx := e / c.perReg
				skillIdx := e % c.perReg
				excludes[e] = fmt.Sprintf("reg%02d-skill-%03d", regIdx%c.registries, skillIdx)
			}
			cfg.Registries = append(cfg.Registries, config.RegistryEntry{
				URL:    "git@example.com:excluder.git",
				Ref:    "main",
				Skills: config.KindSelector{Exclude: excludes},
				Source: config.RepoCommitted,
			})
			setCatalogKeys(cfg)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Resolve(cfg, catalogs, nil)
			}
		})
	}
}

// BenchmarkResolveLocalSelections covers local includes that are already
// resolved and local includes that require scanning configured catalogs.
func BenchmarkResolveLocalSelections(b *testing.B) {
	b.Run("already_resolved", func(b *testing.B) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{URL: "git@example.com:core.git", Ref: "main", Default: true, Source: config.UserPersonal},
			},
		}
		catalogs := map[string][]catalog.Entry{
			cache.CatalogKeyForEntry("git@example.com:core.git", "main", ""): buildNEntries("core", 200),
		}
		setCatalogKeys(cfg)
		localCfg := &config.LocalConfig{
			Skills: config.KindSelector{Include: []string{"core-skill-000", "core-skill-001", "core-skill-050"}},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = Resolve(cfg, catalogs, localCfg)
		}
	})

	b.Run("scan_catalogs", func(b *testing.B) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{URL: "git@example.com:core.git", Ref: "main", Source: config.UserPersonal},
			},
		}
		catalogs := map[string][]catalog.Entry{
			cache.CatalogKeyForEntry("git@example.com:core.git", "main", ""): buildNEntries("core", 200),
		}
		setCatalogKeys(cfg)
		localCfg := &config.LocalConfig{
			Skills: config.KindSelector{Include: []string{"core-skill-150", "core-skill-151"}},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = Resolve(cfg, catalogs, localCfg)
		}
	})
}

// BenchmarkResourcesSelectedTUI covers the per-registry selection path used
// by TUI row construction (ResourcesSelected).
func BenchmarkResourcesSelectedTUI(b *testing.B) {
	cases := []struct {
		name     string
		perReg   int
		required bool
		default_ bool
		tagMatch bool
	}{
		{"required_500_skills", 500, true, false, false},
		{"default_500_skills", 500, false, true, false},
		{"tag_match_500_skills", 500, false, false, true},
		{"explicit_100_skills", 100, false, false, false},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			entries := buildNEntries("tui", c.perReg)
			entry := config.RegistryEntry{
				URL:      "git@example.com:tui.git",
				Ref:      "main",
				Required: c.required,
				Source:   config.UserPersonal,
			}
			if c.default_ {
				entry.Default = true
			}
			if c.tagMatch {
				for i := range entries {
					entries[i].Tags = []string{"tag-0", fmt.Sprintf("tag-%d", i%5)}
				}
				entry.Tags = []string{"tag-0"}
			}
			if !c.required && !c.default_ && !c.tagMatch {
				entry.Skills = config.KindSelector{Include: []string{"tui-skill-000", "tui-skill-050"}}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = ResourcesSelected(entry, entries)
			}
		})
	}
}

// buildNEntries creates n synthetic catalog entries with names like
// "prefix-skill-NNN".
func buildNEntries(prefix string, n int) []catalog.Entry {
	entries := make([]catalog.Entry, n)
	for i := 0; i < n; i++ {
		entries[i] = catalog.Entry{
			Name: fmt.Sprintf("%s-skill-%03d", prefix, i),
			Kind: ir.KindSkill,
			Tags: []string{fmt.Sprintf("tag-%d", i%5)},
			Path: fmt.Sprintf("/cache/%s/skill-%03d", prefix, i),
		}
	}
	return entries
}

// BenchmarkResolveOverlapDedup exercises the overlapping-path merge and
// tiebreak hot path in deduplicateOccurrences. Parent/child registry
// entries sharing the same URL+ref trigger the isPathPrefix check and
// same-scope merge. Mixed-scope variants ensure cross-scope paths stay
// separate. Regression guard for the overlapping-path fix.
func BenchmarkResolveOverlapDedup(b *testing.B) {
	b.Run("same_scope_overlap", func(b *testing.B) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{URL: "git@gitlab.example.com:shared.git", Ref: "main", Path: "", Source: config.Organization, Default: true},
				{URL: "git@gitlab.example.com:shared.git", Ref: "main", Path: "teams/dev", Source: config.Organization, Default: true},
				{URL: "git@gitlab.example.com:shared.git", Ref: "main", Path: "teams/dev/eng", Source: config.Organization, Default: true},
			},
		}
		setCatalogKeys(cfg)

		entries := buildNEntries("overlap", 200)
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: entries,
			cfg.Registries[1].CatalogKey: entries,
			cfg.Registries[2].CatalogKey: entries,
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = Resolve(cfg, catalogs, nil)
		}
	})

	b.Run("mixed_scope_overlap", func(b *testing.B) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{URL: "git@gitlab.example.com:shared.git", Ref: "main", Path: "", Source: config.Organization, Default: true},
				{URL: "git@gitlab.example.com:shared.git", Ref: "main", Path: "teams/dev", Source: config.RepoCommitted, Default: true},
			},
		}
		setCatalogKeys(cfg)

		entries := buildNEntries("mixed", 200)
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: entries,
			cfg.Registries[1].CatalogKey: entries,
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = Resolve(cfg, catalogs, nil)
		}
	})

	b.Run("many_overlapping_paths", func(b *testing.B) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{URL: "git@gitlab.example.com:shared.git", Ref: "main", Path: "", Source: config.UserPersonal, Default: true},
			},
		}
		overlaps := []string{"teams/a", "teams/b", "teams/a/sub", "teams/c"}
		for _, p := range overlaps {
			cfg.Registries = append(cfg.Registries, config.RegistryEntry{
				URL: "git@gitlab.example.com:shared.git", Ref: "main", Path: p, Source: config.UserPersonal, Default: true,
			})
		}
		setCatalogKeys(cfg)

		entries := buildNEntries("many", 100)
		catalogs := map[string][]catalog.Entry{}
		for _, r := range cfg.Registries {
			catalogs[r.CatalogKey] = entries
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = Resolve(cfg, catalogs, nil)
		}
	})
}

// BenchmarkResolveTargetHarnessSet measures the cost of intersecting every
// registry entry's harnesses with a machine-wide target set during resolution.
func BenchmarkResolveTargetHarnessSet(b *testing.B) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "git@example.com:core.git", Ref: "main", Default: true, Source: config.UserPersonal},
		},
	}
	catalogs := map[string][]catalog.Entry{
		cache.CatalogKeyForEntry("git@example.com:core.git", "main", ""): buildNEntries("core", 200),
	}
	setCatalogKeys(cfg)
	targets := []hcc.Target{hcc.TargetClaude, hcc.TargetFactory}
	if got, _ := Resolve(cfg, catalogs, nil, WithTargetHarnesses(targets)); len(got) != 200 {
		b.Fatalf("benchmark fixture resolves %d artifacts, want 200", len(got))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Resolve(cfg, catalogs, nil, WithTargetHarnesses(targets))
	}
}

// BenchmarkResolvePathLessCollectionMembers measures collection expansion when
// members name a monorepo by URL only and many configured entries share that
// URL — the case where binding must find the entry that CONTAINS the member
// instead of taking the first one in config order. Parameterised over the
// number of same-URL entries so a regression on that axis stays visible.
func BenchmarkResolvePathLessCollectionMembers(b *testing.B) {
	const monorepo = "git@example.com:monorepo.git"

	for _, entryCount := range []int{4, 16, 64} {
		b.Run(fmt.Sprintf("entries=%d", entryCount), func(b *testing.B) {
			members := make([]ir.CollectionMember, 0, 8)
			for i := 0; i < 8; i++ {
				members = append(members, ir.CollectionMember{
					Name: fmt.Sprintf("skill-%d", i),
					Kind: ir.KindSkill,
					// Deliberately spelled without ".git" so normalization runs.
					Registry: "git@example.com:monorepo",
				})
			}
			col := ir.NewCollection("bench-tools", "", "", members, nil, nil, ir.SourceRef{})

			cfg := &config.Config{
				Registries: []config.RegistryEntry{{
					URL:         "git@example.com:host.git",
					Ref:         "main",
					Path:        "skills",
					Collections: config.KindSelector{Include: []string{"bench-tools"}},
				}},
			}
			for i := 0; i < entryCount; i++ {
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{
					URL:  monorepo,
					Ref:  "main",
					Path: fmt.Sprintf("teams/plugins/plugin-%d", i),
				})
			}
			setCatalogKeys(cfg)

			catalogs := map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {{
					Kind:     ir.KindCollection,
					Name:     "bench-tools",
					Resource: col,
					Path:     "/cache/host/collections/bench-tools",
				}},
			}
			// Only the last entry provides the members, so every candidate is
			// inspected before the binding succeeds.
			for i := 1; i < len(cfg.Registries); i++ {
				key := cfg.Registries[i].CatalogKey
				catalogs[key] = []catalog.Entry{{Kind: ir.KindSkill, Name: fmt.Sprintf("filler-%d", i), Path: "/cache/filler"}}
			}
			last := cfg.Registries[len(cfg.Registries)-1].CatalogKey
			for i := 0; i < 8; i++ {
				name := fmt.Sprintf("skill-%d", i)
				catalogs[last] = append(catalogs[last], catalog.Entry{Kind: ir.KindSkill, Name: name, Path: "/cache/" + name})
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resolved, _ := Resolve(cfg, catalogs, nil)
				if len(resolved) == 0 {
					b.Fatal("expected resolved artifacts")
				}
			}
		})
	}
}

// BenchmarkResolvePathLessCollectionMembersWorstShape is the adversarial shape
// for path-less collection-member binding: many configured entries for ONE
// monorepo URL, many path-less members, and a large catalog per entry, with the
// providing entry last so every candidate is inspected. This is the worst
// realistic cost of containment-based binding (members x entries URL
// comparisons, plus one map lookup per candidate).
func BenchmarkResolvePathLessCollectionMembersWorstShape(b *testing.B) {
	const monorepo = "git@example.com:monorepo.git"
	const entryCount = 64
	const memberCount = 64
	const catalogSize = 200

	members := make([]ir.CollectionMember, 0, memberCount)
	for i := 0; i < memberCount; i++ {
		members = append(members, ir.CollectionMember{
			Name:     fmt.Sprintf("skill-%d", i),
			Kind:     ir.KindSkill,
			Registry: "git@example.com:monorepo",
		})
	}
	col := ir.NewCollection("bench-tools", "", "", members, nil, nil, ir.SourceRef{})

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:         "git@example.com:host.git",
			Ref:         "main",
			Path:        "skills",
			Collections: config.KindSelector{Include: []string{"bench-tools"}},
		}},
	}
	for i := 0; i < entryCount; i++ {
		cfg.Registries = append(cfg.Registries, config.RegistryEntry{
			URL:  monorepo,
			Ref:  "main",
			Path: fmt.Sprintf("teams/plugins/plugin-%d", i),
		})
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{
			Kind:     ir.KindCollection,
			Name:     "bench-tools",
			Resource: col,
			Path:     "/cache/host/collections/bench-tools",
		}},
	}
	for i := 1; i < len(cfg.Registries); i++ {
		key := cfg.Registries[i].CatalogKey
		entries := make([]catalog.Entry, 0, catalogSize)
		for j := 0; j < catalogSize; j++ {
			entries = append(entries, catalog.Entry{Kind: ir.KindSkill, Name: fmt.Sprintf("filler-%d-%d", i, j), Path: "/cache/filler"})
		}
		catalogs[key] = entries
	}
	last := cfg.Registries[len(cfg.Registries)-1].CatalogKey
	for i := 0; i < memberCount; i++ {
		name := fmt.Sprintf("skill-%d", i)
		catalogs[last] = append(catalogs[last], catalog.Entry{Kind: ir.KindSkill, Name: name, Path: "/cache/" + name})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved, _ := Resolve(cfg, catalogs, nil)
		if len(resolved) != memberCount {
			b.Fatalf("expected %d resolved artifacts, got %d", memberCount, len(resolved))
		}
	}
}

// BenchmarkCandidatesAtPath measures the cost of normalizing and matching a
// collection member's explicit path against configured registry entry paths.
func BenchmarkCandidatesAtPath(b *testing.B) {
	candidates := make([]memberEntryCandidate, 20)
	for i := range candidates {
		candidates[i] = memberEntryCandidate{entry: config.RegistryEntry{Path: fmt.Sprintf("teams/plugin-%d", i)}}
	}
	candidates[10].entry.Path = "teams/development/plugins/dev-language-guides"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = candidatesAtPath(candidates, "teams/development/plugins/dev-language-guides/")
	}
}
