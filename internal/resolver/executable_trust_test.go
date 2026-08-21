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

func TestExplicitlyIncludedHookIsNotWithheldByCollectionExpansion(t *testing.T) {
	col := executableBundle("dev-bundle")
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:     trustRegistry,
		Ref:     "main",
		Default: true,
		Hooks:   ks("bundled-hook"),
		Source:  config.RepoCommitted,
	}}}
	setCatalogKeys(cfg)
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: bundleCatalog(col),
	}, nil)

	if _, ok := resolvedKeys(resolved)["hook:bundled-hook"]; !ok {
		t.Fatalf("an explicit hooks include must still select the hook, got %#v", resolved)
	}
	for _, name := range withheldNames(errs) {
		if name == "hook:bundled-hook" {
			t.Fatal("a hook the config names must not be reported as withheld")
		}
	}
	// An explicit include is a selection, not a broad one: the MCP server the
	// same bundle carries stays withheld.
	if _, ok := resolvedKeys(resolved)["mcpserver:bundled-mcp"]; ok {
		t.Fatal("including one hook must not enable the bundle's MCP server")
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
