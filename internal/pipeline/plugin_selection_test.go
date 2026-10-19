package pipeline

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

func plannedPlugin(name string, source plugindecode.CatalogSource, owner config.RegistryEntry) catalog.PlannedPlugin {
	return catalog.PlannedPlugin{
		Name:   name,
		Source: owner,
		Edges:  []catalog.PluginSelectionEdge{{Type: "direct"}},
		Entry:  catalog.Entry{Kind: ir.KindPlugin, Name: name, Plugin: &catalog.PluginEntry{Format: "claude-plugin", Source: source}},
	}
}

// The plugin-v1 manifest's committed selection is the resolver's own
// selection; the plan carries it and the lock contexts project it per root.
// The resolver-side convergence matrix (TestPlanPluginSelectionMatchesResolve)
// proves plan == Resolve for every selection path.
func TestPluginLockContextsProjectPlan(t *testing.T) {
	decl := config.SourceDecl{Kind: config.V4SourceGit, URL: "https://example.com/cat.git", Ref: "main", Format: "claude-marketplace"}
	entry := registryEntryForDecl(decl)
	external := plannedPlugin("mirror-kit",
		plugindecode.CatalogSource{Kind: "git", URL: "https://git.example.com/mirror.git", Ref: "main"},
		entry)
	plan := catalog.PluginSelectionPlan{Plugins: []catalog.PlannedPlugin{external}}
	canonical, err := lockfile.CanonicalGitConfigURL(entry.URL)
	if err != nil {
		t.Fatal(err)
	}
	doc := lockfile.Document{Registries: []lockfile.GitRegistry{{
		URL: canonical, Ref: "main",
		Roots: []lockfile.GitRoot{{Path: ".", RevisionSchema: "plugin-v1"}},
	}}}

	contexts, err := BuildPluginLockContexts(plan, []config.SourceDecl{decl}, doc, config.RepoCommitted)
	if err != nil {
		t.Fatalf("BuildPluginLockContexts: %v", err)
	}
	if got := contexts.Plans[canonical+"\x00main\x00."]; len(got) != 1 || got[0].Name != "mirror-kit" {
		t.Fatalf("plans = %#v", contexts.Plans)
	}
	externalCanonical, err := lockfile.CanonicalGitConfigURL("https://git.example.com/mirror.git")
	if err != nil {
		t.Fatal(err)
	}
	wantIdentity := externalCanonical + "\x00main\x00."
	if !contexts.Expected[wantIdentity] {
		t.Fatalf("expected external identity missing: %#v", contexts.Expected)
	}
	binding := contexts.Externals[wantIdentity]
	if binding == nil || binding.EntryName != "mirror-kit" || binding.CatalogURL != canonical || binding.CatalogKind != "git" {
		t.Fatalf("external binding = %#v", binding)
	}
}

// Two selected entries in different catalogs that collapse to one external
// record refuse before any lock write.
func TestCheckPlannedSharedOriginsRefusesAcrossCatalogs(t *testing.T) {
	first := plannedPlugin("kit-a",
		plugindecode.CatalogSource{Kind: "git", URL: "https://git.example.com/mirror.git", Ref: "main"},
		config.RegistryEntry{URL: "https://example.com/cat-a.git", Ref: "main", Format: "claude-marketplace"})
	second := plannedPlugin("kit-b",
		plugindecode.CatalogSource{Kind: "git", URL: "https://git.example.com/mirror.git", Ref: "main"},
		config.RegistryEntry{URL: "https://example.com/cat-b.git", Ref: "main", Format: "claude-marketplace"})
	plan := catalog.PluginSelectionPlan{Plugins: []catalog.PlannedPlugin{first, second}}
	if err := CheckPlannedSharedOrigins(plan); err == nil {
		t.Fatal("shared external origin across catalogs must refuse")
	}

	second.Entry.Plugin.Source.Subdirectory = "nested"
	plan = catalog.PluginSelectionPlan{Plugins: []catalog.PlannedPlugin{first, second}}
	if err := CheckPlannedSharedOrigins(plan); err != nil {
		t.Fatalf("distinct subdirectories are distinct records: %v", err)
	}
}
