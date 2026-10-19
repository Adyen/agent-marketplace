package resolver

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

func pluginMembers(pairs ...[2]string) []ir.PluginMember {
	var out []ir.PluginMember
	for _, pair := range pairs {
		var resource ir.Resource
		switch pair[0] {
		case "skill":
			resource = ir.NewSkill(pair[1], "", "", nil, nil, ir.SourceRef{})
		case "command":
			resource = ir.NewCommand(pair[1], "", "", nil, ir.SourceRef{})
		default:
			resource = ir.NewSkill(pair[1], "", "", nil, nil, ir.SourceRef{})
		}
		out = append(out, ir.PluginMember{Resource: resource})
	}
	return out
}

func pluginCatalogEntry(name, format string, members []ir.PluginMember, refusal *diag.Diagnostic, source plugindecode.CatalogSource) catalog.Entry {
	return catalog.Entry{
		Kind: ir.KindPlugin,
		Name: name,
		Plugin: &catalog.PluginEntry{
			Format:  format,
			Source:  source,
			Members: members,
			Refusal: refusal,
		},
	}
}

func pluginDecisions(pairs ...[2]string) []config.V4SelectionDecision {
	var out []config.V4SelectionDecision
	for _, pair := range pairs {
		out = append(out, config.V4SelectionDecision{
			Action:  config.V4SelectionExplicit,
			Locator: pair[0] + "/" + pair[1],
			Request: &config.V4ResourceRequest{Kind: pair[0], Name: pair[1]},
		})
	}
	return out
}

func pluginRequestDecision(name string, opts config.V4ResourceOptions) config.V4SelectionDecision {
	return config.V4SelectionDecision{
		Action:  config.V4SelectionExplicit,
		Locator: "plugin/" + name,
		Request: &config.V4ResourceRequest{Kind: "plugin", Name: name, Options: opts},
	}
}

func formatEntry(format string, decisions []config.V4SelectionDecision) config.RegistryEntry {
	return config.RegistryEntry{
		URL:         "git@example.com:kit.git",
		Ref:         "main",
		V4Kind:      config.V4SourceGit,
		Format:      format,
		V4Decisions: decisions,
	}
}

func pluginArtifacts(resolved []ResolvedArtifact) []ResolvedArtifact {
	var out []ResolvedArtifact
	for _, a := range resolved {
		if a.Kind == ir.KindPlugin {
			out = append(out, a)
		}
	}
	return out
}

func memberKeys(members []ir.PluginMember) []string {
	var out []string
	for _, m := range members {
		out = append(out, m.Key())
	}
	return out
}

func errStrings(errs []error) string {
	var b strings.Builder
	for _, e := range errs {
		b.WriteString(e.Error())
		b.WriteString("\n")
	}
	return b.String()
}

func TestResolveSelectsPluginWithDirectEdgeAndMembers(t *testing.T) {
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "review-kit"}))
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	members := pluginMembers([2]string{"skill", "review"}, [2]string{"command", "legacy-review"})
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("review-kit", "claude-plugin", members, nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
	}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 1 || plugins[0].Name != "review-kit" {
		t.Fatalf("expected one plugin artifact, got %#v", resolved)
	}
	sel := plugins[0].Selection
	if sel == nil {
		t.Fatalf("plugin artifact carries no selection metadata: %#v", plugins[0])
	}
	if len(sel.Edges) != 1 || sel.Edges[0].Kind != PluginEdgeDirect {
		t.Fatalf("plugin edges = %#v", sel.Edges)
	}
	if got := memberKeys(sel.Members); len(got) != 2 || got[0] != "skill/review" || got[1] != "command/legacy-review" {
		t.Fatalf("plugin members = %v", got)
	}
}

func TestResolveSelectsStructuredPluginEntry(t *testing.T) {
	decision := pluginRequestDecision("1password.tools", config.V4ResourceOptions{})
	decision.Request.Structured = true
	entry := formatEntry("agent-plugin", []config.V4SelectionDecision{decision})
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("1password.tools", "agent-plugin",
			pluginMembers([2]string{"skill", "vault"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "."}),
	}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 1 || plugins[0].Name != "1password.tools" {
		t.Fatalf("structured plugin entry not selected: %#v", resolved)
	}
}

// spec: plugin-packages/direct-selection-of-a-plugin-member-is-refused
func TestResolveRefusesDirectPluginMemberSelection(t *testing.T) {
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"skill", "review"}))
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("review-kit", "claude-plugin",
			pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
	}}

	_, errs := Resolve(cfg, catalogs, nil)
	text := errStrings(errs)
	if !strings.Contains(text, "plugin.member-direct-selection") || !strings.Contains(text, "plugin/review-kit") {
		t.Fatalf("expected plugin.member-direct-selection naming plugin/review-kit, got: %v", errs)
	}
}

// spec: plugin-packages/exclusion-of-an-unknown-member-is-refused
func TestResolveRefusesUnknownMemberExclusion(t *testing.T) {
	decision := pluginRequestDecision("review-kit", config.V4ResourceOptions{Exclude: []string{"command/nope"}})
	entry := formatEntry("claude-marketplace", []config.V4SelectionDecision{decision})
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("review-kit", "claude-plugin",
			pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
	}}

	_, errs := Resolve(cfg, catalogs, nil)
	text := errStrings(errs)
	if !strings.Contains(text, "plugin.member.unknown-exclusion") || !strings.Contains(text, "command/nope") {
		t.Fatalf("expected plugin.member.unknown-exclusion naming command/nope, got: %v", errs)
	}
}

func TestResolveExcludesNamedPluginMember(t *testing.T) {
	decision := pluginRequestDecision("review-kit", config.V4ResourceOptions{Exclude: []string{"command/legacy-review", "skill/vault"}})
	entry := formatEntry("claude-marketplace", []config.V4SelectionDecision{decision})
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("review-kit", "claude-plugin",
			pluginMembers([2]string{"skill", "review"}, [2]string{"skill", "vault"}, [2]string{"command", "legacy-review"}), nil,
			plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
	}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 1 {
		t.Fatalf("expected one plugin, got %#v", resolved)
	}
	got := memberKeys(plugins[0].Selection.Members)
	if len(got) != 1 || got[0] != "skill/review" {
		t.Fatalf("excluded members still planned: %v", got)
	}
}

func TestResolveExclusionDoesNotMutateCatalogMembers(t *testing.T) {
	decision := pluginRequestDecision("review-kit", config.V4ResourceOptions{Exclude: []string{"skill/review"}})
	entry := formatEntry("claude-marketplace", []config.V4SelectionDecision{decision})
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	entry0 := pluginCatalogEntry("review-kit", "claude-plugin",
		pluginMembers([2]string{"skill", "review"}, [2]string{"command", "legacy-review"}), nil,
		plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"})
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {entry0}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 1 {
		t.Fatalf("expected one plugin, got %#v", resolved)
	}
	if got := memberKeys(plugins[0].Selection.Members); len(got) != 1 || got[0] != "command/legacy-review" {
		t.Fatalf("excluded member still planned: %v", got)
	}
	got := memberKeys(catalogs[cfg.Registries[0].CatalogKey][0].Plugin.Members)
	if len(got) != 2 || got[0] != "skill/review" || got[1] != "command/legacy-review" {
		t.Fatalf("exclusion mutated the catalog member slice: %v", got)
	}
}

func TestResolveAllSelectsPluginsMinusGroupExclusion(t *testing.T) {
	entry := formatEntry("claude-marketplace", []config.V4SelectionDecision{
		{Action: config.V4SelectionExclude, Locator: "plugin/npm-kit", Exclusion: "plugin/npm-kit"},
	})
	entry.V4All = true
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("review-kit", "claude-plugin",
			pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
		pluginCatalogEntry("npm-kit", "claude-plugin",
			pluginMembers([2]string{"skill", "lint"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/npm-kit"}),
	}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 1 || plugins[0].Name != "review-kit" {
		t.Fatalf("all:true plus exclude plugin/npm-kit must resolve only review-kit: %#v", resolved)
	}
}

// spec: plugin-packages/same-named-members-of-two-plugins-stay-distinct
func TestResolveSameNamedMembersStayDistinct(t *testing.T) {
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "review-kit"}, [2]string{"plugin", "lint-kit"}))
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("review-kit", "claude-plugin",
			pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
		pluginCatalogEntry("lint-kit", "claude-plugin",
			pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/lint-kit"}),
	}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 2 {
		t.Fatalf("expected two plugin artifacts, got %#v", resolved)
	}
	for _, p := range plugins {
		if p.Selection == nil || len(p.Selection.Members) != 1 || p.Selection.Members[0].Key() != "skill/review" {
			t.Fatalf("plugin %q lost its member: %#v", p.Name, p.Selection)
		}
	}
	if plugins[0].Name == plugins[1].Name {
		t.Fatalf("plugins collapsed: %#v", plugins)
	}
}

// spec: plugin-packages/matching-loose-resource-is-not-deduplicated-against-a-member
func TestResolveLooseResourceNotDeduplicatedAgainstMember(t *testing.T) {
	loose := config.RegistryEntry{URL: "git@example.com:loose.git", Ref: "main", Skills: ks("review")}
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "review-kit"}))
	cfg := &config.Config{Registries: []config.RegistryEntry{loose, entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindSkill, Name: "review"}},
		cfg.Registries[1].CatalogKey: {
			pluginCatalogEntry("review-kit", "claude-plugin",
				pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	var looseSkill, plugin ResolvedArtifact
	for _, a := range resolved {
		if a.Kind == ir.KindSkill && a.Name == "review" {
			looseSkill = a
		}
		if a.Kind == ir.KindPlugin {
			plugin = a
		}
	}
	if looseSkill.Name == "" || plugin.Name == "" {
		t.Fatalf("loose skill and plugin member must both be planned, got %#v (errs=%v)", resolved, errs)
	}
}

// spec: plugin-packages/refused-entry-source-type-blocks-only-that-entry
func TestResolveRefusedEntryBlocksOnlyThatEntry(t *testing.T) {
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "npm-kit"}, [2]string{"plugin", "review-kit"}))
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	refusal := &diag.Diagnostic{Code: diag.CodePluginCatalogSourceUnsupported, Severity: diag.SeverityWarning, Resource: "npm-kit", Message: "source type refused"}
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("npm-kit", "claude-plugin", nil, refusal, plugindecode.CatalogSource{}),
		pluginCatalogEntry("review-kit", "claude-plugin",
			pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
	}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	text := errStrings(errs)
	if !strings.Contains(text, "plugin.catalog.source-unsupported") || !strings.Contains(text, "npm-kit") {
		t.Fatalf("expected the entry diagnostic for npm-kit, got: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 1 || plugins[0].Name != "review-kit" {
		t.Fatalf("installable sibling must still resolve: %#v", resolved)
	}
}

func TestResolveExternalCatalogEntryResolvesToPluginArtifact(t *testing.T) {
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "mirror-kit"}))
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("mirror-kit", "claude-plugin", nil, nil,
			plugindecode.CatalogSource{Kind: "git", URL: "https://git.example.com/mirror.git", Ref: "main"}),
	}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("external entry selection must resolve, got: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 1 || plugins[0].Name != "mirror-kit" {
		t.Fatalf("the external plugin must resolve to a plugin artifact: %#v", resolved)
	}
}

func TestResolvePluginViaCollectionGetsCollectionEdge(t *testing.T) {
	colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("team")}
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "review-kit"}))
	cfg := &config.Config{Registries: []config.RegistryEntry{colEntry, entry}}
	setCatalogKeys(cfg)
	col := ir.NewCollection("team", "", "",
		[]ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git"}},
		nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "team", Resource: col}},
		cfg.Registries[1].CatalogKey: {
			pluginCatalogEntry("review-kit", "claude-plugin",
				pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"}),
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors: %v", errs)
	}
	plugins := pluginArtifacts(resolved)
	if len(plugins) != 1 {
		t.Fatalf("expected one plugin selected both ways, got %#v", resolved)
	}
	edges := plugins[0].Selection.Edges
	var direct, collection bool
	for _, e := range edges {
		if e.Kind == PluginEdgeDirect {
			direct = true
		}
		if e.Kind == PluginEdgeCollection && e.Collection == "team" {
			collection = true
		}
	}
	if !direct || !collection {
		t.Fatalf("plugin selected directly and by a collection must carry both edges: %#v", edges)
	}
}
