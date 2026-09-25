package resolver

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func effectiveSelectionEntry(mutators ...func(*config.RegistryEntry)) config.RegistryEntry {
	entry := config.RegistryEntry{
		URL:        "git@example.com:reg.git",
		Ref:        "main",
		Source:     config.RepoCommitted,
		CatalogKey: "catalog",
		Skills:     config.KindSelector{Include: []string{"picked"}},
	}
	for _, mutate := range mutators {
		mutate(&entry)
	}
	return entry
}

func TestEffectiveDirectSelectionSourcesEntryPolicy(t *testing.T) {
	explicit := &config.V4ResourceRequest{Kind: "skill", Name: "picked"}
	cases := []struct {
		name    string
		entries []config.RegistryEntry
		want    bool
	}{
		{
			name: "legacy include selects",
			entries: []config.RegistryEntry{
				effectiveSelectionEntry(),
			},
			want: true,
		},
		{
			name: "legacy include and exclude suppresses",
			entries: []config.RegistryEntry{
				effectiveSelectionEntry(func(e *config.RegistryEntry) {
					e.Skills.Exclude = []string{"picked"}
				}),
			},
			want: false,
		},
		{
			name: "exclude all suppresses",
			entries: []config.RegistryEntry{
				effectiveSelectionEntry(func(e *config.RegistryEntry) {
					e.Skills.ExcludeAll = true
				}),
			},
			want: false,
		},
		{
			name: "required immunity keeps include",
			entries: []config.RegistryEntry{
				effectiveSelectionEntry(func(e *config.RegistryEntry) {
					e.Required = true
					e.Skills.ExcludeAll = true
					e.Skills.Exclude = []string{"picked"}
				}),
			},
			want: true,
		},
		{
			name: "v4 include then exclude suppresses",
			entries: []config.RegistryEntry{
				effectiveSelectionEntry(func(e *config.RegistryEntry) {
					e.V4Decisions = []config.V4SelectionDecision{
						{Action: config.V4SelectionExplicit, Request: explicit},
						{Action: config.V4SelectionExclude, Exclusion: "skill/picked"},
					}
				}),
			},
			want: false,
		},
		{
			name: "v4 exclude then include reselects",
			entries: []config.RegistryEntry{
				effectiveSelectionEntry(func(e *config.RegistryEntry) {
					e.V4Decisions = []config.V4SelectionDecision{
						{Action: config.V4SelectionExclude, Exclusion: "skill/picked"},
						{Action: config.V4SelectionExplicit, Request: explicit},
					}
				}),
			},
			want: true,
		},
		{
			name: "v4 required immunity keeps include",
			entries: []config.RegistryEntry{
				effectiveSelectionEntry(func(e *config.RegistryEntry) {
					e.Required = true
					e.V4Decisions = []config.V4SelectionDecision{
						{Action: config.V4SelectionExplicit, Request: explicit},
						{Action: config.V4SelectionExclude, Exclusion: "skill/picked"},
					}
				}),
			},
			want: true,
		},
		{
			name: "unselected name",
			entries: []config.RegistryEntry{
				effectiveSelectionEntry(),
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := "picked"
			if tc.name == "unselected name" {
				name = "other"
			}
			entrySelected, _ := EffectiveDirectSelectionSources(tc.entries[0], tc.entries, nil, ir.KindSkill, name)
			if entrySelected != tc.want {
				t.Fatalf("entrySelected = %v, want %v", entrySelected, tc.want)
			}
		})
	}
}

func TestEffectiveDirectSelectionSourcesHigherAuthorityShadowing(t *testing.T) {
	lower := effectiveSelectionEntry()
	lower.FilePrecedence = 1
	higher := config.RegistryEntry{
		URL:            lower.URL,
		Ref:            lower.Ref,
		Source:         config.RepoCommitted,
		FilePrecedence: 2,
		Skills:         config.KindSelector{Exclude: []string{"picked"}},
	}
	entries := []config.RegistryEntry{lower, higher}
	entrySelected, _ := EffectiveDirectSelectionSources(entries[0], entries, nil, ir.KindSkill, "picked")
	if entrySelected {
		t.Fatal("shadowed selection reported as effective")
	}

	requiredLower := lower
	requiredLower.Required = true
	entrySelected, _ = EffectiveDirectSelectionSources(requiredLower, []config.RegistryEntry{requiredLower, higher}, nil, ir.KindSkill, "picked")
	if !entrySelected {
		t.Fatal("required selection was shadowed")
	}
}

func TestEffectiveDirectSelectionSourcesLocalPolicy(t *testing.T) {
	entry := effectiveSelectionEntry()
	unselected := effectiveSelectionEntry(func(e *config.RegistryEntry) { e.Skills.Include = nil })

	cases := []struct {
		name      string
		entry     config.RegistryEntry
		local     *config.LocalConfig
		wantEntry bool
		wantLocal bool
	}{
		{
			name:      "top-level local include selects",
			entry:     unselected,
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}}},
			wantLocal: true,
		},
		{
			name:      "local include suppressed by local exclude",
			entry:     unselected,
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}, Exclude: []string{"picked"}}},
			wantLocal: false,
		},
		{
			name:      "local include suppressed by exclude all",
			entry:     unselected,
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}, ExcludeAll: true}},
			wantLocal: false,
		},
		{
			name:      "local include suppressed by entry exclude",
			entry:     effectiveSelectionEntry(func(e *config.RegistryEntry) { e.Skills.Exclude = []string{"picked"} }),
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}}},
			wantLocal: false,
		},
		{
			name: "required owner keeps local selection",
			entry: effectiveSelectionEntry(func(e *config.RegistryEntry) {
				e.Required = true
				e.Skills.Exclude = []string{"picked"}
			}),
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}, Exclude: []string{"picked"}}},
			wantEntry: true,
			wantLocal: true,
		},
		{
			name: "v4 exclude then include keeps local selection",
			entry: effectiveSelectionEntry(func(e *config.RegistryEntry) {
				e.V4Decisions = []config.V4SelectionDecision{
					{Action: config.V4SelectionExclude, Exclusion: "skill/picked"},
					{Action: config.V4SelectionExplicit, Request: &config.V4ResourceRequest{Kind: "skill", Name: "picked"}},
				}
			}),
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}}},
			wantEntry: true,
			wantLocal: true,
		},
		{
			name: "v4 include then exclude suppresses local selection",
			entry: effectiveSelectionEntry(func(e *config.RegistryEntry) {
				e.V4Decisions = []config.V4SelectionDecision{
					{Action: config.V4SelectionExplicit, Request: &config.V4ResourceRequest{Kind: "skill", Name: "picked"}},
					{Action: config.V4SelectionExclude, Exclusion: "skill/picked"},
				}
			}),
			local: &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}}},
		},
		{
			name: "v4 wildcard exclude then include keeps local selection",
			entry: effectiveSelectionEntry(func(e *config.RegistryEntry) {
				e.V4Decisions = []config.V4SelectionDecision{
					{Action: config.V4SelectionExclude, Exclusion: "skill/*"},
					{Action: config.V4SelectionExplicit, Request: &config.V4ResourceRequest{Kind: "skill", Name: "picked"}},
				}
			}),
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}}},
			wantEntry: true,
			wantLocal: true,
		},
		{
			name: "v4 reinclude suppressed by local exclude",
			entry: effectiveSelectionEntry(func(e *config.RegistryEntry) {
				e.V4Decisions = []config.V4SelectionDecision{
					{Action: config.V4SelectionExclude, Exclusion: "skill/picked"},
					{Action: config.V4SelectionExplicit, Request: &config.V4ResourceRequest{Kind: "skill", Name: "picked"}},
				}
			}),
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}, Exclude: []string{"picked"}}},
			wantEntry: true,
		},
		{
			name: "v4 required owner keeps local reinclude",
			entry: effectiveSelectionEntry(func(e *config.RegistryEntry) {
				e.Required = true
				e.V4Decisions = []config.V4SelectionDecision{
					{Action: config.V4SelectionExplicit, Request: &config.V4ResourceRequest{Kind: "skill", Name: "picked"}},
					{Action: config.V4SelectionExclude, Exclusion: "skill/picked"},
				}
			}),
			local:     &config.LocalConfig{Skills: config.KindSelector{Include: []string{"picked"}, Exclude: []string{"picked"}}},
			wantEntry: true,
			wantLocal: true,
		},
		{
			name:      "no local include does not select",
			entry:     entry,
			local:     &config.LocalConfig{},
			wantEntry: true,
			wantLocal: false,
		},
		{
			name:      "nil local config does not select",
			entry:     entry,
			wantEntry: true,
			wantLocal: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := []config.RegistryEntry{tc.entry}
			entrySelected, localSelected := EffectiveDirectSelectionSources(entries[0], entries, tc.local, ir.KindSkill, "picked")
			if entrySelected != tc.wantEntry || localSelected != tc.wantLocal {
				t.Fatalf("got (%v, %v), want (%v, %v)", entrySelected, localSelected, tc.wantEntry, tc.wantLocal)
			}
		})
	}
}
