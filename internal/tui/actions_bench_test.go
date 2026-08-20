package tui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkBuildRows measures the hot-path cost of buildRows under two
// representative scenarios:
//   - "normal": distinct skill names across two registries.
//   - "same_name_across_registries": identical skill name in two registries.
func BenchmarkBuildRows(b *testing.B) {
	b.Run("normal", func(b *testing.B) {
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{
					URL: "git@example.com:org/security.git", Ref: "main",
					Skills: ks("audit-logging"), Harnesses: []string{"factory", "cursor"},
					Source: config.RepoCommitted, CatalogKey: "sec-key",
				},
				{
					URL: "git@example.com:org/default.git", Ref: "main",
					Skills: ks("default-skill"), Harnesses: []string{"factory"},
					Source: config.RepoCommitted, CatalogKey: "default-key",
				},
			},
		}
		catalogs := map[string]catalogInfo{
			"sec-key": {Entries: []catalog.Entry{
				{Name: "audit-logging", Path: "/cache/sec/audit-logging"},
				{Name: "extra-security", Path: "/cache/sec/extra-security"},
			}},
			"default-key": {Entries: []catalog.Entry{
				{Name: "default-skill", Path: "/cache/default/default-skill"},
			}},
		}
		resolved := []resolver.ResolvedArtifact{
			{Entry: catalog.Entry{Name: "audit-logging"}, RegistryURL: "git@example.com:org/security.git", CatalogKey: "sec-key"},
			{Entry: catalog.Entry{Name: "default-skill"}, RegistryURL: "git@example.com:org/default.git", CatalogKey: "default-key"},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{})
		}
	})

	b.Run("same_name_across_registries", func(b *testing.B) {
		localRoot := b.TempDir()
		cfg := &config.Config{
			Registries: []config.RegistryEntry{
				{
					URL: "git@example.com:org/primary.git", Ref: "main",
					Skills: ks("smith"),
					Source: config.UserPersonal, CatalogKey: "primary-key",
				},
				{
					URL: localRoot, Path: "skills",
					Source: config.RepoCommitted, CatalogKey: "smith-local-key",
				},
			},
		}
		catalogs := map[string]catalogInfo{
			"primary-key": {Entries: []catalog.Entry{
				{Name: "smith", Path: "/cache/primary/smith"},
			}},
			"smith-local-key": {Entries: []catalog.Entry{
				{Name: "smith", Path: filepath.Join(localRoot, "skills", "smith")},
			}},
		}
		resolved := []resolver.ResolvedArtifact{
			{Entry: catalog.Entry{Name: "smith"}, RegistryURL: "git@example.com:org/primary.git", CatalogKey: "primary-key"},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{})
		}
	})
}

// BenchmarkBuildRowsLargeMixed covers hundreds to thousands of resources
// across registries, kinds, drift states, local excludes, and same-name
// resources.
func BenchmarkBuildRowsLargeMixed(b *testing.B) {
	cases := []struct {
		name      string
		regs      int
		perReg    int
		sameNames bool
	}{
		{"500_resources_5regs", 5, 100, false},
		{"1000_resources_10regs", 10, 100, false},
		{"2000_resources_20regs", 20, 100, false},
		{"500_same_name", 5, 100, true},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			cfg := &config.Config{}
			catalogs := make(map[string]catalogInfo)
			var resolved []resolver.ResolvedArtifact
			localCfg := &config.LocalConfig{
				Skills: config.KindSelector{
					Exclude: []string{"resource-0002"},
				},
				Commands: config.KindSelector{
					Include: []string{"resource-0000"},
				},
				Subagents: config.KindSelector{
					Exclude: []string{"resource-0001"},
				},
			}
			driftReport := materialize.DriftReport{
				Missing: []materialize.DriftItem{{Name: skill.ResourceKey(ir.KindSkill, "resource-0002"), Reason: materialize.DriftMissing}},
				Broken:  []materialize.DriftItem{{Name: skill.ResourceKey(ir.KindCommand, "resource-0010"), Reason: materialize.DriftBroken}},
			}

			for r := 0; r < c.regs; r++ {
				url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
				key := fmt.Sprintf("key-%d", r)
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{
					URL: url, Ref: "main",
					Default: true,
					Source:  config.RepoCommitted, CatalogKey: key,
				})
				entries := make([]catalog.Entry, c.perReg)
				for s := 0; s < c.perReg; s++ {
					name := fmt.Sprintf("resource-%04d", r*c.perReg+s)
					if c.sameNames {
						name = fmt.Sprintf("resource-%04d", s)
					}
					var kind ir.ResourceKind
					switch s % 10 {
					case 0:
						kind = ir.KindCommand
					case 1:
						kind = ir.KindSubagent
					default:
						kind = ir.KindSkill
					}
					entries[s] = catalog.Entry{
						Kind: kind,
						Name: name,
						Tags: []string{fmt.Sprintf("tag-%d", s%10)},
						Path: fmt.Sprintf("/cache/reg-%02d/%s", r, name),
					}
					resolved = append(resolved, resolver.ResolvedArtifact{
						Entry:       entries[s],
						RegistryURL: url,
						Ref:         "main",
						Source:      config.RepoCommitted,
						CatalogKey:  key,
					})
				}
				catalogs[key] = catalogInfo{Entries: entries}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buildRows(cfg, catalogs, resolved, localCfg, driftReport)
			}
		})
	}
}

// BenchmarkRecomputeView covers scope, registry, and status tabs with empty
// and active filters.
func BenchmarkRecomputeView(b *testing.B) {
	rows := buildBenchRows(500)

	cases := []struct {
		name   string
		tab    tab
		filter string
	}{
		{"scope_empty_filter", tabScope, ""},
		{"scope_active_filter", tabScope, "security"},
		{"registry_empty_filter", tabRegistry, ""},
		{"registry_active_filter", tabRegistry, "helm"},
		{"status_empty_filter", tabStatus, ""},
		{"status_active_filter", tabStatus, "go"},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			m := NewModel("/repo", "/home")
			m.rows = append([]row(nil), rows...)
			m.activeTab = c.tab
			m.filter = c.filter

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.recomputeView()
			}
		})
	}
}

// BenchmarkRenderListVisibleRows covers visible heights of 25, 50, and 100
// rows.
func BenchmarkRenderListVisibleRows(b *testing.B) {
	rows := buildBenchRows(500)
	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "wide_25", width: 120, height: 25},
		{name: "wide_50", width: 120, height: 50},
		{name: "wide_100", width: 120, height: 100},
		{name: "wrapped_25", width: 30, height: 25},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			m := NewModel("/repo", "/home")
			m.width = c.width
			m.height = c.height + 3
			m.loading = false
			m.rows = append([]row(nil), rows...)
			m.recomputeView()
			m.listOffset = 0

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = renderList(m, c.height)
			}
		})
	}
}

func BenchmarkClampListOffset(b *testing.B) {
	b.Run("cache_hit", func(b *testing.B) {
		m := benchmarkClampModel()
		m.listHeightOffsets(20)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.cursor = (m.cursor + 1) % len(m.flatItems)
			m.clampListOffset()
		}
	})
	b.Run("resize_rebuild", func(b *testing.B) {
		m := benchmarkClampModel()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.width = 39 + i%2
			m.cursor = (m.cursor + 1) % len(m.flatItems)
			m.clampListOffset()
		}
	})
}

func BenchmarkRenderBaseScreen(b *testing.B) {
	r := mkRow("selected-resource-with-a-long-name", withProject, withRegistry("https://example.com/a/very/long/registry/location.git", "registry-with-a-long-name"))
	r.Artifact.Path = "/a/very/long/cache/location/that/must-wrap/SKILL.md"
	r.Artifact.Description = "long description content that wraps in the detail pane"
	m := modelWithSelectedRow(b, r)
	m.width = 80
	m.height = 24

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.renderBaseScreen()
	}
}

func BenchmarkRenderDetail(b *testing.B) {
	r := mkRow("selected-resource-with-a-long-name", withProject, withRegistry("https://example.com/a/very/long/registry/location.git", "registry-with-a-long-name"))
	r.Artifact.Path = "/a/very/long/cache/location/that/must-wrap/SKILL.md"
	r.Artifact.Description = "long description content that wraps in the detail pane"
	m := modelWithSelectedRow(b, r)
	m.width = 80

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderDetail(m)
	}
}

func benchmarkClampModel() Model {
	m := NewModel("/repo", "/home")
	m.width = 50
	m.height = 28
	m.flatItems = make([]flatItem, 500)
	for i := range m.flatItems {
		m.flatItems[i] = flatItem{Kind: flatSection, SectionTitle: fmt.Sprintf("long-section-title-%03d-with-wrapped-content", i)}
	}
	return m
}

var benchTuiSrvOnce sync.Once
var benchTuiSrv *httptest.Server

func benchTuiServer() *httptest.Server {
	benchTuiSrvOnce.Do(func() {
		benchTuiSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		}))
		if _, err := http.Get(benchTuiSrv.URL); err != nil {
			panic(err)
		}
	})
	return benchTuiSrv
}

func BenchmarkRecordActionSpan(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchTuiServer()
	user := &config.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: srv.URL,
		},
	}

	state, err := telemetry.ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		b.Fatal(err)
	}
	tel, err := telemetry.New(context.Background(), state, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		b.StopTimer()
		_ = tel.Shutdown(context.Background())
	}()

	ctx := telemetry.NewContext(context.Background(), tel, "tui", time.Now(), "factory", false, telemetry.Span{}, false)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recordActionSpan(ctx, "tui.action.sync", map[string]string{"resource.kind": "skill"})
	}
}
