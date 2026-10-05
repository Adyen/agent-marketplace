package tui

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func unavailableEntry(mutators ...func(*config.RegistryEntry)) config.RegistryEntry {
	entry := config.RegistryEntry{
		URL:        "/reg",
		Source:     config.RepoCommitted,
		CatalogKey: "k1",
		Skills:     config.KindSelector{Include: []string{"missing"}},
	}
	for _, mutate := range mutators {
		mutate(&entry)
	}
	return entry
}

func unavailableSelectionErr(name string) error {
	return &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: name, URL: "/reg"}
}

func TestAppendUnavailableSelectionRowsEffectiveEntryPolicy(t *testing.T) {
	explicit := &config.V4ResourceRequest{Kind: "skill", Name: "missing"}
	loadedCatalogs := map[string]session.CatalogInfo{"k1": {}}
	cases := []struct {
		name    string
		entry   config.RegistryEntry
		wantRow bool
	}{
		{name: "legacy include", entry: unavailableEntry(), wantRow: true},
		{
			name: "legacy include and exclude",
			entry: unavailableEntry(func(e *config.RegistryEntry) {
				e.Skills.Exclude = []string{"missing"}
			}),
		},
		{
			name: "exclude all",
			entry: unavailableEntry(func(e *config.RegistryEntry) {
				e.Skills.ExcludeAll = true
			}),
		},
		{
			name: "required immunity",
			entry: unavailableEntry(func(e *config.RegistryEntry) {
				e.Required = true
				e.Skills.ExcludeAll = true
			}),
			wantRow: true,
		},
		{
			name: "v4 include then exclude",
			entry: unavailableEntry(func(e *config.RegistryEntry) {
				e.V4Decisions = []config.V4SelectionDecision{
					{Action: config.V4SelectionExplicit, Request: explicit},
					{Action: config.V4SelectionExclude, Exclusion: "skill/missing"},
				}
			}),
		},
		{
			name: "v4 exclude then include",
			entry: unavailableEntry(func(e *config.RegistryEntry) {
				e.V4Decisions = []config.V4SelectionDecision{
					{Action: config.V4SelectionExclude, Exclusion: "skill/missing"},
					{Action: config.V4SelectionExplicit, Request: explicit},
				}
			}),
			wantRow: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Registries: []config.RegistryEntry{tc.entry}}
			rows := appendUnavailableSelectionRows(nil, cfg, nil, []error{unavailableSelectionErr("missing")}, loadedCatalogs)
			if got := len(rows) == 1; got != tc.wantRow {
				t.Fatalf("rows = %d, wantRow %v", len(rows), tc.wantRow)
			}
		})
	}
}

func TestAppendUnavailableSelectionRowsShadowedEntry(t *testing.T) {
	loadedCatalogs := map[string]session.CatalogInfo{"k1": {}}
	lower := unavailableEntry(func(e *config.RegistryEntry) { e.FilePrecedence = 1 })
	higher := config.RegistryEntry{
		URL:            "/reg",
		Source:         config.RepoCommitted,
		FilePrecedence: 2,
		CatalogKey:     "k1",
		Skills:         config.KindSelector{Exclude: []string{"missing"}},
	}
	cfg := &config.Config{Registries: []config.RegistryEntry{lower, higher}}
	rows := appendUnavailableSelectionRows(nil, cfg, nil, []error{unavailableSelectionErr("missing")}, loadedCatalogs)
	if len(rows) != 0 {
		t.Fatalf("shadowed selection synthesized %d rows", len(rows))
	}
}

func TestAppendUnavailableSelectionRowsFailedCatalogTopLevelLocal(t *testing.T) {
	failedEntry := unavailableEntry(func(e *config.RegistryEntry) {
		e.Skills.Include = nil
	})
	cfg := &config.Config{Registries: []config.RegistryEntry{failedEntry}}
	localCfg := &config.LocalConfig{Skills: config.KindSelector{Include: []string{"missing"}}}
	rows := appendUnavailableSelectionRows(nil, cfg, localCfg, nil, map[string]session.CatalogInfo{})
	if len(rows) != 1 {
		t.Fatalf("top-level local include synthesized %d rows, want 1", len(rows))
	}
	if !rows[0].EnabledLocal || rows[0].EnabledGlobal || rows[0].EnabledProject {
		t.Fatalf("local recovery row scope flags = global:%v project:%v local:%v, want local only",
			rows[0].EnabledGlobal, rows[0].EnabledProject, rows[0].EnabledLocal)
	}
	if rows[0].UnavailableReason != session.UnavailableCatalogReason {
		t.Fatalf("local recovery row reason = %q, want %q", rows[0].UnavailableReason, session.UnavailableCatalogReason)
	}

	localCfg.Skills.Exclude = []string{"missing"}
	rows = appendUnavailableSelectionRows(nil, cfg, localCfg, nil, map[string]session.CatalogInfo{})
	if len(rows) != 0 {
		t.Fatalf("local include+exclude synthesized %d rows, want 0", len(rows))
	}
}

func TestAppendUnavailableSelectionRowsFailedCatalogV4ReincludeKeepsLocalFlag(t *testing.T) {
	failedEntry := unavailableEntry(func(e *config.RegistryEntry) {
		e.Skills.Include = nil
		e.V4Decisions = []config.V4SelectionDecision{
			{Action: config.V4SelectionExclude, Exclusion: "skill/missing"},
			{Action: config.V4SelectionExplicit, Request: &config.V4ResourceRequest{Kind: "skill", Name: "missing"}},
		}
	})
	cfg := &config.Config{Registries: []config.RegistryEntry{failedEntry}}
	localCfg := &config.LocalConfig{Skills: config.KindSelector{Include: []string{"missing"}}}
	rows := appendUnavailableSelectionRows(nil, cfg, localCfg, nil, map[string]session.CatalogInfo{})
	if len(rows) != 1 {
		t.Fatalf("v4 reinclude with local include synthesized %d rows, want 1", len(rows))
	}
	if !rows[0].EnabledLocal || !rows[0].EnabledProject {
		t.Fatalf("v4 reinclude row scope flags = project:%v local:%v, want project and local",
			rows[0].EnabledProject, rows[0].EnabledLocal)
	}

	failedEntry.V4Decisions = []config.V4SelectionDecision{
		{Action: config.V4SelectionExplicit, Request: &config.V4ResourceRequest{Kind: "skill", Name: "missing"}},
		{Action: config.V4SelectionExclude, Exclusion: "skill/missing"},
	}
	cfg.Registries[0] = failedEntry
	rows = appendUnavailableSelectionRows(nil, cfg, localCfg, nil, map[string]session.CatalogInfo{})
	if len(rows) != 0 {
		t.Fatalf("v4 include then exclude synthesized %d rows, want 0", len(rows))
	}
}

func TestAppendUnavailableSelectionRowsFailedCatalogLocalIncludeCoveredByAvailableRow(t *testing.T) {
	failedEntry := unavailableEntry(func(e *config.RegistryEntry) {
		e.URL = "/regA"
		e.CatalogKey = "kA"
		e.Skills.Include = nil
	})
	healthyEntry := config.RegistryEntry{URL: "/regB", Source: config.RepoCommitted, CatalogKey: "kB"}
	cfg := &config.Config{Registries: []config.RegistryEntry{failedEntry, healthyEntry}}
	localCfg := &config.LocalConfig{Skills: config.KindSelector{Include: []string{"shared"}}}
	initial := []row{{
		IsAvailable: true,
		Artifact:    resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: "/regB"},
	}}
	rows := appendUnavailableSelectionRows(initial, cfg, localCfg, nil, map[string]session.CatalogInfo{"kB": {}})
	if len(rows) != 1 {
		t.Fatalf("available-covered local include synthesized %d rows, want 0", len(rows)-1)
	}
}

func TestAppendUnavailableSelectionRowsFailedCatalogLocalIncludeEntrySelectionStillRecovered(t *testing.T) {
	failedEntry := unavailableEntry(func(e *config.RegistryEntry) {
		e.URL = "/regA"
		e.CatalogKey = "kA"
		e.Skills.Include = []string{"shared"}
	})
	cfg := &config.Config{Registries: []config.RegistryEntry{failedEntry}}
	initial := []row{{
		IsAvailable: true,
		Artifact:    resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared"}, RegistryURL: "/regB"},
	}}
	rows := appendUnavailableSelectionRows(initial, cfg, nil, nil, map[string]session.CatalogInfo{})
	if len(rows) != 2 || rows[1].RegistryURL != "/regA" {
		t.Fatalf("explicit entry selection on failed catalog lost its recovery row: %#v", rows)
	}
}

func TestAppendUnavailableSelectionRowsLocalIncludeMultipleFailedIdentities(t *testing.T) {
	entryA := unavailableEntry(func(e *config.RegistryEntry) {
		e.URL = "/regA"
		e.CatalogKey = "kA"
		e.Skills.Include = nil
	})
	entryB := unavailableEntry(func(e *config.RegistryEntry) {
		e.URL = "/regB"
		e.CatalogKey = "kB"
		e.Skills.Include = nil
	})
	cfg := &config.Config{Registries: []config.RegistryEntry{entryA, entryB}}
	localCfg := &config.LocalConfig{Skills: config.KindSelector{Include: []string{"missing"}}}
	rows := appendUnavailableSelectionRows(nil, cfg, localCfg, nil, map[string]session.CatalogInfo{})
	if len(rows) != 0 {
		t.Fatalf("local include with two failed registry identities synthesized %d rows, want 0", len(rows))
	}
}

func TestAppendUnavailableSelectionRowsLocalIncludeMultipleFailedIdentitiesEntrySelectionsStillRecovered(t *testing.T) {
	entryA := unavailableEntry(func(e *config.RegistryEntry) {
		e.URL = "/regA"
		e.CatalogKey = "kA"
	})
	entryB := unavailableEntry(func(e *config.RegistryEntry) {
		e.URL = "/regB"
		e.CatalogKey = "kB"
	})
	cfg := &config.Config{Registries: []config.RegistryEntry{entryA, entryB}}
	localCfg := &config.LocalConfig{Skills: config.KindSelector{Include: []string{"missing"}}}
	rows := appendUnavailableSelectionRows(nil, cfg, localCfg, nil, map[string]session.CatalogInfo{})
	if len(rows) != 2 {
		t.Fatalf("explicit entry selections on two failed catalogs synthesized %d rows, want 2", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.RegistryURL] = true
	}
	for _, url := range []string{"/regA", "/regB"} {
		if !seen[url] {
			t.Fatalf("ambiguous local suppression dropped the %q entry-owned recovery row: %#v", url, rows)
		}
	}
}

func TestAppendUnavailableSelectionRowsLocalIncludeDuplicateFailedIdentity(t *testing.T) {
	first := unavailableEntry(func(e *config.RegistryEntry) {
		e.Skills.Include = nil
	})
	dup := unavailableEntry(func(e *config.RegistryEntry) {
		e.CatalogKey = "k2"
		e.Source = config.RepoPersonal
		e.Skills.Include = nil
	})
	cfg := &config.Config{Registries: []config.RegistryEntry{first, dup}}
	localCfg := &config.LocalConfig{Skills: config.KindSelector{Include: []string{"missing"}}}
	rows := appendUnavailableSelectionRows(nil, cfg, localCfg, nil, map[string]session.CatalogInfo{})
	if len(rows) != 1 {
		t.Fatalf("duplicate failed entries sharing one identity synthesized %d rows, want 1", len(rows))
	}
	if rows[0].RegistryURL != "/reg" {
		t.Fatalf("recovery row registry = %q, want representative /reg", rows[0].RegistryURL)
	}
}

func TestAppendUnavailableSelectionRowsFailedCatalogEffectiveRequests(t *testing.T) {
	failedEntry := unavailableEntry(func(e *config.RegistryEntry) {
		e.Skills.Include = []string{"kept", "dropped"}
		e.Skills.Exclude = []string{"dropped"}
	})
	cfg := &config.Config{Registries: []config.RegistryEntry{failedEntry}}
	rows := appendUnavailableSelectionRows(nil, cfg, nil, nil, map[string]session.CatalogInfo{})
	if len(rows) != 1 || rows[0].Artifact.Name != "kept" {
		t.Fatalf("failed catalog sweep synthesized %#v, want only the effective selection", rows)
	}
}
