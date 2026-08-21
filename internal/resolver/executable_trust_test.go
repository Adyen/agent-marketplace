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

	// A top-level `collections: include:` names no registry, so it cannot say
	// whose executables were accepted. It selects the collection (skills expand)
	// and vouches for nothing — including when only one configured registry
	// publishes the name, because which registries happen to be configured is
	// not a statement the user made about which one they trusted. The exploit
	// this closes: a personal config carrying a legacy top-level name, and a
	// repository that adds the only registry publishing a bundle under it.
	for _, publishers := range []string{"sole_publisher", "two_publishers"} {
		t.Run("top_level_name_never_vouches_"+publishers, func(t *testing.T) {
			cfg := &config.Config{
				Collections: ks("dev-bundle"),
				Registries: []config.RegistryEntry{
					{URL: "git@example.com:untrusted.git", Ref: "main", Source: config.RepoCommitted},
					{URL: trustRegistry, Ref: "main", Source: config.UserPersonal},
				},
			}
			setCatalogKeys(cfg)
			catalogs := map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: bundleCatalog(executableBundle("dev-bundle")),
				cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "unrelated", Path: "/cache/skills/unrelated"}},
			}
			if publishers == "two_publishers" {
				catalogs[cfg.Registries[1].CatalogKey] = bundleCatalog(newTrustCollection("dev-bundle", []ir.CollectionMember{
					{Kind: ir.KindSkill, Name: "safe-skill"},
				}))
			}
			resolved, errs := Resolve(cfg, catalogs, nil)

			byKey := resolvedKeys(resolved)
			for _, key := range []string{"hook:bundled-hook", "mcpserver:bundled-mcp"} {
				if _, ok := byKey[key]; ok {
					t.Fatalf("a top-level name must not vouch for any provider, got %#v", byKey)
				}
			}
			// Selection still works: the named bundle expands its safe members.
			if _, ok := byKey["skill:safe-skill"]; !ok {
				t.Fatalf("expected the named collection to still expand its skills, got %#v", byKey)
			}

			var reported *ExecutableCollectionMemberWarning
			for _, err := range errs {
				var withheld *ExecutableCollectionMemberWarning
				if errors.As(err, &withheld) && len(withheld.TopLevelProviders) > 0 {
					reported = withheld
					break
				}
			}
			if reported == nil {
				t.Fatalf("expected the withholding to report the top-level name, got %v", errs)
			}
			// The user did name the collection, so telling them to name it would
			// be advice they already took; the missing piece is the provider.
			if strings.Contains(reported.Remediation(), "select the collection by name on the registry entry") {
				t.Fatalf("remediation must address the missing provider, got %q", reported.Remediation())
			}
			for _, want := range []string{"untrusted", "onto the registry entry you meant"} {
				if !strings.Contains(reported.Error()+reported.Remediation(), want) {
					t.Fatalf("expected %q in %q / %q", want, reported.Error(), reported.Remediation())
				}
			}
		})
	}

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

// An `includes:` entry is a bare name, so whoever answers it decides what the
// include delivers. The including collection's own provider answers first:
// otherwise a registry that publishes a same-named bundle could substitute its
// content — and its hooks — for the one the author meant, under trust the user
// granted to somebody else.
func TestNestedIncludeBindsToIncludingProvider(t *testing.T) {
	// Catalog keys are set by hand so the hijacker sorts first: the binding used
	// to be "lowest catalog key wins", which is what this pins against.
	const (
		hijackerKey  = "aaa-hijacker"
		includingKey = "zzz-including"
	)
	outer := newTrustCollection("outer", nil, "inner")
	ownInner := newTrustCollection("inner", []ir.CollectionMember{{Kind: ir.KindHook, Name: "own-hook"}})
	hijackInner := newTrustCollection("inner", []ir.CollectionMember{{Kind: ir.KindHook, Name: "evil-hook"}})

	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.com:hijacker.git", Ref: "main", Default: true, Source: config.RepoCommitted, CatalogKey: hijackerKey},
		{URL: trustRegistry, Ref: "main", Collections: ks("outer"), Source: config.UserPersonal, CatalogKey: includingKey},
	}}
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
		hijackerKey: {
			{Kind: ir.KindCollection, Name: "inner", Resource: hijackInner},
			{Kind: ir.KindHook, Name: "evil-hook", Path: "/cache/hijacker/hooks/evil-hook"},
		},
		includingKey: {
			{Kind: ir.KindCollection, Name: "outer", Resource: outer},
			{Kind: ir.KindCollection, Name: "inner", Resource: ownInner},
			{Kind: ir.KindHook, Name: "own-hook", Path: "/cache/team/hooks/own-hook"},
		},
	}, nil)

	byKey := resolvedKeys(resolved)
	if _, ok := byKey["hook:evil-hook"]; ok {
		t.Fatalf("another registry's same-named collection answered the include, got %#v", byKey)
	}
	if _, ok := byKey["hook:own-hook"]; !ok {
		t.Fatalf("expected the including provider's own nested hook to be enabled, got %#v (errors: %v)", byKey, errs)
	}
}

// Trust never crosses a provider boundary: naming a collection accepts what its
// own registry composes into it, not whatever a registry the author does not
// control publishes under a name they referenced.
func TestCrossProviderIncludeDoesNotInheritTrust(t *testing.T) {
	newCfg := func(sharedProviderNamesIt bool) (*config.Config, map[string][]catalog.Entry) {
		outer := newTrustCollection("outer", nil, "shared")
		shared := newTrustCollection("shared", []ir.CollectionMember{
			{Kind: ir.KindSkill, Name: "shared-skill"},
			{Kind: ir.KindHook, Name: "shared-hook"},
		})
		sharedEntry := config.RegistryEntry{URL: "git@example.com:shared.git", Ref: "main", Source: config.RepoCommitted}
		if sharedProviderNamesIt {
			sharedEntry.Collections = ks("shared")
		}
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: trustRegistry, Ref: "main", Collections: ks("outer"), Source: config.UserPersonal},
			sharedEntry,
		}}
		setCatalogKeys(cfg)
		return cfg, map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "outer", Resource: outer}},
			cfg.Registries[1].CatalogKey: {
				{Kind: ir.KindCollection, Name: "shared", Resource: shared},
				{Kind: ir.KindSkill, Name: "shared-skill", Path: "/cache/shared/skills/shared-skill"},
				{Kind: ir.KindHook, Name: "shared-hook", Path: "/cache/shared/hooks/shared-hook"},
			},
		}
	}

	t.Run("withheld_and_explained", func(t *testing.T) {
		cfg, catalogs := newCfg(false)
		resolved, errs := Resolve(cfg, catalogs, nil)

		byKey := resolvedKeys(resolved)
		if _, ok := byKey["hook:shared-hook"]; ok {
			t.Fatalf("a cross-provider include must not inherit trust, got %#v", byKey)
		}
		// Composition still works for everything that cannot execute.
		if _, ok := byKey["skill:shared-skill"]; !ok {
			t.Fatalf("expected the included collection's skill to still expand, got %#v (errors: %v)", byKey, errs)
		}

		var reported *ExecutableCollectionMemberWarning
		for _, err := range errs {
			var withheld *ExecutableCollectionMemberWarning
			if errors.As(err, &withheld) && withheld.Name == "shared-hook" {
				reported = withheld
				break
			}
		}
		if reported == nil {
			t.Fatalf("expected the withholding to be reported, got %v", errs)
		}
		if reported.IncludedFrom == "" {
			t.Fatalf("expected the warning to name the including provider, got %#v", reported)
		}
		if !strings.Contains(reported.Error(), "does not cross a provider boundary") {
			t.Fatalf("message must explain why nothing was inherited, got %q", reported.Error())
		}
		// The remediation has to point at the collection that carries the hook,
		// not at the outer one the user already named.
		if !strings.Contains(reported.Remediation(), "[shared]") {
			t.Fatalf("remediation must name the included collection, got %q", reported.Remediation())
		}
		if !strings.Contains(reported.Error(), "shared.git") || !strings.Contains(reported.Remediation(), "shared.git") {
			t.Fatalf("expected the publishing registry to be named, got %q / %q", reported.Error(), reported.Remediation())
		}
	})

	t.Run("enabled_by_its_own_provider_scoped_selection", func(t *testing.T) {
		cfg, catalogs := newCfg(true)
		resolved, errs := Resolve(cfg, catalogs, nil)

		if names := withheldNames(errs); len(names) != 0 {
			t.Fatalf("naming the included collection on its own provider enables it, got withheld %v", names)
		}
		if _, ok := resolvedKeys(resolved)["hook:shared-hook"]; !ok {
			t.Fatalf("expected the nested hook to be enabled, got %#v", resolved)
		}
	})
}

// Installing a collection writes a per-entry `collections: include:`, which is a
// trust record rather than a narrowing: it must not silently deselect every skill
// the registry provides, or users are pushed back to the routes this rule closes.
func TestCollectionIncludeKeepsBroadSelection(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:         trustRegistry,
		Ref:         "main",
		Default:     true,
		Collections: ks("dev-bundle"),
		Source:      config.RepoCommitted,
	}}}
	setCatalogKeys(cfg)
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: append(bundleCatalog(executableBundle("dev-bundle")),
			catalog.Entry{Kind: ir.KindSkill, Name: "other-skill", Path: "/cache/skills/other-skill"}),
	}, nil)

	byKey := resolvedKeys(resolved)
	if _, ok := byKey["skill:other-skill"]; !ok {
		t.Fatalf("naming a collection must not switch off the entry's broad selection, got %#v", byKey)
	}
	assertBundleFullyEnabled(t, resolved, errs)
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
	for _, want := range []string{"collections: include: [dev-bundle]", "mcp: include", "install --kind mcp bundled-mcp"} {
		if !strings.Contains(remediation, want) {
			t.Fatalf("remediation %q missing %q", remediation, want)
		}
	}
}
