package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Hooks and MCP servers execute commands, so they are only enabled by a
// deliberate selection. A collection is the interesting case: the bundle is
// authored in the registry, so expanding one that was merely swept up by
// `default: true` or by a tag would enable executables that appear in no config
// — the same silent enablement the per-kind rule forbids on the entry itself.

const (
	trustRegistry    = "git@example.com:team.git"
	trustOrgRegistry = "git@example.com:org/policy.git"
)

// newTrustCollection builds a collection resource with the given members.
func newTrustCollection(name string, members []ir.CollectionMember, includes ...string) *ir.Collection {
	return ir.NewCollection(name, "1.0", "trust fixture", members, includes, nil, ir.SourceRef{})
}

// executableBundle is a collection carrying one safe and two executable members.
func executableBundle(name string) *ir.Collection {
	return newTrustCollection(name, []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "safe-skill"},
		{Kind: ir.KindHook, Name: "bundled-hook"},
		{Kind: ir.KindMcpServer, Name: "bundled-mcp"},
	})
}

// bundleCatalog is the catalog for a registry that publishes executableBundle
// and every resource it names.
func bundleCatalog(col *ir.Collection, tags ...string) []catalog.Entry {
	return []catalog.Entry{
		{Kind: ir.KindCollection, Name: col.Name(), Resource: col, Tags: tags, Path: "/cache/collections/" + col.Name()},
		{Kind: ir.KindSkill, Name: "safe-skill", Tags: tags, Path: "/cache/skills/safe-skill"},
		{Kind: ir.KindHook, Name: "bundled-hook", Tags: tags, Path: "/cache/hooks/bundled-hook"},
		{Kind: ir.KindMcpServer, Name: "bundled-mcp", Tags: tags, Path: "/cache/mcp/bundled-mcp"},
	}
}

// resolvedKeys indexes resolved artifacts by "<kind>:<name>".
func resolvedKeys(resolved []ResolvedArtifact) map[string]ResolvedArtifact {
	out := make(map[string]ResolvedArtifact, len(resolved))
	for _, artifact := range resolved {
		out[ViaMapKey(artifact.Kind, artifact.Name)] = artifact
	}
	return out
}

// withheldNames returns the resources reported as withheld executables.
func withheldNames(errs []error) []string {
	var names []string
	for _, err := range errs {
		var withheld *ExecutableCollectionMemberWarning
		if errors.As(err, &withheld) {
			names = append(names, ViaMapKey(withheld.Kind, withheld.Name))
		}
	}
	return names
}

func TestCollectionUnderBroadSelectionWithholdsExecutables(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry config.RegistryEntry
		tags  []string
	}{
		{
			name:  "default_true",
			entry: config.RegistryEntry{URL: trustRegistry, Ref: "main", Default: true, Source: config.RepoCommitted},
		},
		{
			name:  "tag_match",
			entry: config.RegistryEntry{URL: trustRegistry, Ref: "main", Tags: []string{"dev"}, Source: config.RepoCommitted},
			tags:  []string{"dev"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			col := executableBundle("dev-bundle")
			cfg := &config.Config{Registries: []config.RegistryEntry{tc.entry}}
			setCatalogKeys(cfg)
			resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: bundleCatalog(col, tc.tags...),
			}, nil)

			byKey := resolvedKeys(resolved)
			if _, ok := byKey["skill:safe-skill"]; !ok {
				t.Fatalf("expected the collection's safe member to still expand, got %#v", byKey)
			}
			for _, key := range []string{"hook:bundled-hook", "mcpserver:bundled-mcp"} {
				if _, ok := byKey[key]; ok {
					t.Fatalf("%s was enabled by a broadly selected collection", key)
				}
			}

			got := withheldNames(errs)
			if len(got) != 2 {
				t.Fatalf("expected one withholding warning per executable member, got %v (all errors: %v)", got, errs)
			}
			for _, err := range errs {
				if !IsWarning(err) {
					t.Fatalf("withholding must not fail a sync, got hard error %v", err)
				}
			}
		})
	}
}

func TestNamedCollectionEnablesExecutables(t *testing.T) {
	t.Run("entry_selector", func(t *testing.T) {
		col := executableBundle("dev-bundle")
		cfg := &config.Config{Registries: []config.RegistryEntry{{
			URL:         trustRegistry,
			Ref:         "main",
			Collections: ks("dev-bundle"),
			Source:      config.RepoCommitted,
		}}}
		setCatalogKeys(cfg)
		resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: bundleCatalog(col),
		}, nil)
		assertBundleFullyEnabled(t, resolved, errs)
	})

	t.Run("top_level_selector", func(t *testing.T) {
		col := executableBundle("dev-bundle")
		cfg := &config.Config{
			Collections: ks("dev-bundle"),
			Registries: []config.RegistryEntry{{
				URL:    trustRegistry,
				Ref:    "main",
				Source: config.RepoCommitted,
			}},
		}
		setCatalogKeys(cfg)
		resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: bundleCatalog(col),
		}, nil)
		assertBundleFullyEnabled(t, resolved, errs)
	})

	t.Run("required_organization_registry", func(t *testing.T) {
		col := executableBundle("dev-bundle")
		cfg := &config.Config{Registries: []config.RegistryEntry{{
			URL:      trustOrgRegistry,
			Ref:      "main",
			Required: true,
			Source:   config.Organization,
		}}}
		setCatalogKeys(cfg)
		resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: bundleCatalog(col),
		}, nil)
		assertBundleFullyEnabled(t, resolved, errs)
	})
}

func assertBundleFullyEnabled(t *testing.T, resolved []ResolvedArtifact, errs []error) {
	t.Helper()
	if names := withheldNames(errs); len(names) != 0 {
		t.Fatalf("expected no withholding, got %v", names)
	}
	byKey := resolvedKeys(resolved)
	for _, key := range []string{"skill:safe-skill", "hook:bundled-hook", "mcpserver:bundled-mcp"} {
		if _, ok := byKey[key]; !ok {
			t.Fatalf("expected %s to be enabled, got %#v", key, byKey)
		}
	}
}

func TestNestedCollectionInheritsExecutableTrust(t *testing.T) {
	inner := newTrustCollection("inner", []ir.CollectionMember{
		{Kind: ir.KindHook, Name: "bundled-hook"},
	})
	outer := newTrustCollection("outer", nil, "inner")

	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:         trustRegistry,
		Ref:         "main",
		Collections: ks("outer"),
		Source:      config.RepoCommitted,
	}}}
	setCatalogKeys(cfg)
	key := cfg.Registries[0].CatalogKey
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{key: {
		{Kind: ir.KindCollection, Name: "outer", Resource: outer},
		{Kind: ir.KindCollection, Name: "inner", Resource: inner},
		{Kind: ir.KindHook, Name: "bundled-hook"},
	}}, nil)

	if names := withheldNames(errs); len(names) != 0 {
		t.Fatalf("naming the outer collection accepts what it composes, got withheld %v", names)
	}
	if _, ok := resolvedKeys(resolved)["hook:bundled-hook"]; !ok {
		t.Fatalf("expected the nested hook to be enabled, got %#v", resolved)
	}
}

func TestNestedCollectionUnderBroadSelectionWithholdsOnce(t *testing.T) {
	inner := newTrustCollection("inner", []ir.CollectionMember{
		{Kind: ir.KindHook, Name: "bundled-hook"},
	})
	outer := newTrustCollection("outer", nil, "inner")

	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:     trustRegistry,
		Ref:     "main",
		Default: true,
		Source:  config.RepoCommitted,
	}}}
	setCatalogKeys(cfg)
	key := cfg.Registries[0].CatalogKey
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{key: {
		{Kind: ir.KindCollection, Name: "outer", Resource: outer},
		{Kind: ir.KindCollection, Name: "inner", Resource: inner},
		{Kind: ir.KindHook, Name: "bundled-hook"},
	}}, nil)

	if _, ok := resolvedKeys(resolved)["hook:bundled-hook"]; ok {
		t.Fatal("a nested collection must not widen trust under broad selection")
	}
	// The hook is reachable both directly (inner is auto-selected too) and
	// through outer; the user has one decision to make about it.
	if names := withheldNames(errs); len(names) != 1 {
		t.Fatalf("expected exactly one withholding warning, got %v", names)
	}
}

// A collection name is not a globally unique identifier. Naming "dev-bundle" on
// one registry entry must not vouch for a same-named bundle published by any
// other registry, or a repository that adds a broadly-selected registry could
// have its own hooks enabled by a selection the user wrote for someone else.
func TestCollectionTrustIsKeyedByProvider(t *testing.T) {
	t.Run("entry_selector_does_not_vouch_for_another_registry", func(t *testing.T) {
		cfg := &config.Config{Registries: []config.RegistryEntry{
			// Added by the repository: broadly selected, publishes its own
			// bundle under a name the user's config happens to name.
			{URL: "git@example.com:untrusted.git", Ref: "main", Default: true, Source: config.RepoCommitted},
			// The entry the user actually named the collection on.
			{URL: trustRegistry, Ref: "main", Collections: ks("dev-bundle"), Source: config.UserPersonal},
		}}
		setCatalogKeys(cfg)
		resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: bundleCatalog(executableBundle("dev-bundle")),
			cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "unrelated", Path: "/cache/skills/unrelated"}},
		}, nil)

		byKey := resolvedKeys(resolved)
		for _, key := range []string{"hook:bundled-hook", "mcpserver:bundled-mcp"} {
			if _, ok := byKey[key]; ok {
				t.Fatalf("%s was enabled by a selection naming a different registry's collection", key)
			}
		}
		// The bundle still expands what it may: only executables are withheld.
		if _, ok := byKey["skill:safe-skill"]; !ok {
			t.Fatalf("expected the collection's safe member to expand, got %#v", byKey)
		}
		if names := withheldNames(errs); len(names) != 2 {
			t.Fatalf("expected both executables to be reported as withheld, got %v (all errors: %v)", names, errs)
		}
	})

	t.Run("top_level_name_answered_by_two_registries_fails_closed", func(t *testing.T) {
		cfg := &config.Config{
			Collections: ks("dev-bundle"),
			Registries: []config.RegistryEntry{
				{URL: trustRegistry, Ref: "main", Source: config.UserPersonal},
				{URL: "git@example.com:untrusted.git", Ref: "main", Source: config.RepoCommitted},
			},
		}
		setCatalogKeys(cfg)
		resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: bundleCatalog(newTrustCollection("dev-bundle", []ir.CollectionMember{
				{Kind: ir.KindSkill, Name: "safe-skill"},
			})),
			cfg.Registries[1].CatalogKey: bundleCatalog(executableBundle("dev-bundle")),
		}, nil)

		if _, ok := resolvedKeys(resolved)["hook:bundled-hook"]; ok {
			t.Fatalf("a top-level name two registries answer to must not vouch for either, got %#v", resolved)
		}

		var ambiguous *ExecutableCollectionMemberWarning
		for _, err := range errs {
			var withheld *ExecutableCollectionMemberWarning
			if errors.As(err, &withheld) && len(withheld.AmbiguousProviders) > 0 {
				ambiguous = withheld
				break
			}
		}
		if ambiguous == nil {
			t.Fatalf("expected the withholding to report the ambiguity, got %v", errs)
		}
		if len(ambiguous.AmbiguousProviders) != 2 {
			t.Fatalf("expected both providers to be named, got %v", ambiguous.AmbiguousProviders)
		}
		// The user did name the collection, so telling them to name it would be
		// advice they already took.
		if strings.Contains(ambiguous.Remediation(), "select the collection by name") {
			t.Fatalf("remediation must address the ambiguity, got %q", ambiguous.Remediation())
		}
		for _, want := range []string{"untrusted", "registry entry you meant"} {
			if !strings.Contains(ambiguous.Error()+ambiguous.Remediation(), want) {
				t.Fatalf("expected %q in %q / %q", want, ambiguous.Error(), ambiguous.Remediation())
			}
		}
	})

	t.Run("nested_include_by_bare_name_does_not_widen_trust", func(t *testing.T) {
		inner := newTrustCollection("inner", []ir.CollectionMember{{Kind: ir.KindHook, Name: "bundled-hook"}})
		outer := newTrustCollection("outer", nil, "inner")
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: "git@example.com:untrusted.git", Ref: "main", Default: true, Source: config.RepoCommitted},
			// The user names "inner" on their own registry, which does not
			// publish it; the untrusted registry does.
			{URL: trustRegistry, Ref: "main", Collections: ks("inner"), Source: config.UserPersonal},
		}}
		setCatalogKeys(cfg)
		resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {
				{Kind: ir.KindCollection, Name: "outer", Resource: outer},
				{Kind: ir.KindCollection, Name: "inner", Resource: inner},
				{Kind: ir.KindHook, Name: "bundled-hook", Path: "/cache/hooks/bundled-hook"},
			},
			cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "unrelated", Path: "/cache/skills/unrelated"}},
		}, nil)

		if _, ok := resolvedKeys(resolved)["hook:bundled-hook"]; ok {
			t.Fatalf("an include chain must not be trusted by bare name, got %#v", resolved)
		}
		if names := withheldNames(errs); len(names) != 1 {
			t.Fatalf("expected exactly one withholding warning, got %v (all errors: %v)", names, errs)
		}
	})
}

func TestCollectionMemberPinnedToRequiredRegistryEnablesExecutable(t *testing.T) {
	col := newTrustCollection("dev-bundle", []ir.CollectionMember{
		{Kind: ir.KindHook, Name: "org-hook", Registry: trustOrgRegistry},
	})

	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: trustRegistry, Ref: "main", Default: true, Source: config.RepoCommitted},
		{URL: trustOrgRegistry, Ref: "main", Required: true, Source: config.Organization},
	}}
	setCatalogKeys(cfg)
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "dev-bundle", Resource: col}},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindHook, Name: "org-hook", Path: "/cache/org/hooks/org-hook"}},
	}, nil)

	if names := withheldNames(errs); len(names) != 0 {
		t.Fatalf("an organization-required hook is already broadly selected, got withheld %v", names)
	}
	if _, ok := resolvedKeys(resolved)["hook:org-hook"]; !ok {
		t.Fatalf("expected the required registry's hook to be enabled, got %#v", resolved)
	}
}

// Naming a hook must not cost the user everything else the registry provides.
// A hook or MCP server include is the only way to name an executable at all, so
// it says nothing about the kinds it does not mention: broad selection keeps
// running, the collection still expands, and only the executables nobody named
// stay withheld.
func TestExplicitlyIncludedHookIsNotWithheldByCollectionExpansion(t *testing.T) {
	// The bundle reaches into a second registry that no selection touches, so a
	// member resolved from there proves expansion actually ran rather than the
	// entry's broad selection picking the same names up directly.
	col := newTrustCollection("dev-bundle", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "safe-skill"},
		{Kind: ir.KindHook, Name: "bundled-hook"},
		{Kind: ir.KindMcpServer, Name: "bundled-mcp"},
		{Kind: ir.KindSkill, Name: "remote-skill", Registry: trustOrgRegistry},
	})
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{
			URL:     trustRegistry,
			Ref:     "main",
			Default: true,
			Hooks:   ks("bundled-hook"),
			Source:  config.RepoCommitted,
		},
		{URL: trustOrgRegistry, Ref: "main", Source: config.UserPersonal},
	}}
	setCatalogKeys(cfg)
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: bundleCatalog(col),
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "remote-skill", Path: "/cache/org/skills/remote-skill"}},
	}, nil)

	byKey := resolvedKeys(resolved)
	if _, ok := byKey["hook:bundled-hook"]; !ok {
		t.Fatalf("an explicit hooks include must still select the hook, got %#v", byKey)
	}
	// The regression this pins: an executable include used to switch the entry
	// into explicit-selection mode, which silently deselected every skill and
	// left the collection unexpanded.
	if _, ok := byKey["skill:safe-skill"]; !ok {
		t.Fatalf("naming a hook must not switch off the entry's broad selection of skills, got %#v", byKey)
	}
	member, expanded := byKey["skill:remote-skill"]
	if !expanded {
		t.Fatalf("expected the collection to expand its cross-registry member, got %#v", byKey)
	}
	if member.Via != "collection:dev-bundle@1.0" {
		t.Fatalf("expected the member to carry collection provenance, got %q", member.Via)
	}
	if member.CatalogKey != cfg.Registries[1].CatalogKey {
		t.Fatalf("expected the member to bind to the registry the collection names, got %q", member.CatalogKey)
	}

	for _, name := range withheldNames(errs) {
		if name == "hook:bundled-hook" {
			t.Fatal("a hook the config names must not be reported as withheld")
		}
	}
	// An explicit include is a selection of that resource, not a vouch for the
	// bundle: the MCP server the same collection carries stays withheld.
	if _, ok := byKey["mcpserver:bundled-mcp"]; ok {
		t.Fatal("including one hook must not enable the bundle's MCP server")
	}
	if names := withheldNames(errs); len(names) != 1 || names[0] != "mcpserver:bundled-mcp" {
		t.Fatalf("expected exactly the MCP server to be reported as withheld, got %v (all errors: %v)", names, errs)
	}
}

// A non-executable include still means "these instead of everything": the
// per-kind carve-out is only for kinds broad selection never reaches.
func TestNonExecutableIncludeStillNarrowsBroadSelection(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:     trustRegistry,
		Ref:     "main",
		Default: true,
		Skills:  ks("safe-skill"),
		Hooks:   ks("bundled-hook"),
		Source:  config.RepoCommitted,
	}}}
	setCatalogKeys(cfg)
	resolved, _ := Resolve(cfg, map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: append(bundleCatalog(executableBundle("dev-bundle")),
			catalog.Entry{Kind: ir.KindSkill, Name: "other-skill", Path: "/cache/skills/other-skill"}),
	}, nil)

	byKey := resolvedKeys(resolved)
	if _, ok := byKey["skill:other-skill"]; ok {
		t.Fatalf("a skills include must still narrow the selection, got %#v", byKey)
	}
	for _, key := range []string{"skill:safe-skill", "hook:bundled-hook"} {
		if _, ok := byKey[key]; !ok {
			t.Fatalf("expected %s to be selected, got %#v", key, byKey)
		}
	}
}

// A required organization registry does select executables broadly, so there an
// executable include is a genuine narrowing and keeps its old meaning.
func TestRequiredEntryExecutableIncludeStillNarrows(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:      trustOrgRegistry,
		Ref:      "main",
		Required: true,
		Hooks:    ks("bundled-hook"),
		Source:   config.Organization,
	}}}
	setCatalogKeys(cfg)
	resolved, _ := Resolve(cfg, map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: bundleCatalog(executableBundle("dev-bundle")),
	}, nil)

	byKey := resolvedKeys(resolved)
	if len(byKey) != 1 {
		t.Fatalf("expected the required entry's include to narrow to one resource, got %#v", byKey)
	}
	if _, ok := byKey["hook:bundled-hook"]; !ok {
		t.Fatalf("expected the named hook, got %#v", byKey)
	}
}

// Closing the implicit routes must not close the permitted opt-out: a hook the
// repository selected by name is still excludable from the personal layer, while
// organization policy stays immune.
func TestPersonalExcludeStillRemovesExplicitlySelectedExecutables(t *testing.T) {
	catalogEntries := []catalog.Entry{
		{Kind: ir.KindHook, Name: "bundled-hook", Path: "/cache/hooks/bundled-hook"},
		{Kind: ir.KindMcpServer, Name: "bundled-mcp", Path: "/cache/mcp/bundled-mcp"},
	}
	localCfg := &config.LocalConfig{
		Hooks: config.KindSelector{Exclude: []string{"bundled-hook"}},
		MCP:   config.KindSelector{ExcludeAll: true},
	}

	t.Run("repository_selection", func(t *testing.T) {
		cfg := &config.Config{Registries: []config.RegistryEntry{{
			URL:    trustRegistry,
			Ref:    "main",
			Hooks:  ks("bundled-hook"),
			MCP:    ks("bundled-mcp"),
			Source: config.RepoCommitted,
		}}}
		setCatalogKeys(cfg)
		resolved, _ := Resolve(cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: catalogEntries,
		}, localCfg)
		if len(resolved) != 0 {
			t.Fatalf("expected the personal layer to exclude both executables, got %#v", resolved)
		}
	})

	t.Run("organization_policy", func(t *testing.T) {
		cfg := &config.Config{Registries: []config.RegistryEntry{{
			URL:      trustOrgRegistry,
			Ref:      "main",
			Required: true,
			Source:   config.Organization,
		}}}
		setCatalogKeys(cfg)
		resolved, _ := Resolve(cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: catalogEntries,
		}, localCfg)
		if len(resolved) != len(catalogEntries) {
			t.Fatalf("required organization resources are immune to personal excludes, got %#v", resolved)
		}
	})
}

func TestWithheldExecutableWarningIsActionable(t *testing.T) {
	warning := &ExecutableCollectionMemberWarning{
		Kind: ir.KindMcpServer,
		Name: "bundled-mcp",
		Via:  "collection:dev-bundle@1.0",
		URL:  trustRegistry,
	}
	if !IsWarning(warning) || !IsExecutableCollectionMemberWithheld(warning) {
		t.Fatal("withholding must classify as a soft warning")
	}
	msg := warning.Error()
	for _, want := range []string{"bundled-mcp", "collection:dev-bundle@1.0", "not enabled"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
	remediation := warning.Remediation()
	for _, want := range []string{"collections: include", "mcp: include", "install --kind mcp bundled-mcp"} {
		if !strings.Contains(remediation, want) {
			t.Fatalf("remediation %q missing %q", remediation, want)
		}
	}
}
