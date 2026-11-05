package resolver

import (
	"errors"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: resource-selection/unqualified-member-retains-the-collection-owner
// spec: resource-selection/unqualified-required-member-retains-required-policy
func TestResolveUnqualifiedCollectionMemberRetainsOwner(t *testing.T) {
	for _, kind := range []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent, ir.KindHook, ir.KindMcpServer} {
		for _, tc := range []struct {
			name     string
			earlier  config.RegistryEntry
			owner    config.RegistryEntry
			excluded bool
		}{
			{
				name:    "repository collection with earlier user entry",
				earlier: config.RegistryEntry{Source: config.UserPersonal},
				owner:   config.RegistryEntry{Source: config.RepoCommitted},
			},
			{
				name:    "user collection with earlier repository entry",
				earlier: config.RegistryEntry{Source: config.RepoCommitted},
				owner:   config.RegistryEntry{Source: config.UserPersonal},
			},
			{
				name:     "required organization collection with optional user entry",
				earlier:  config.RegistryEntry{Source: config.UserPersonal},
				owner:    config.RegistryEntry{Source: config.Organization, Required: true},
				excluded: true,
			},
			{
				name:     "required local collection with optional repository entry",
				earlier:  config.RegistryEntry{Source: config.RepoCommitted},
				owner:    config.RegistryEntry{Source: config.RepoPersonal, Required: true},
				excluded: true,
			},
			{
				name:    "including file selects collection over fragment",
				earlier: config.RegistryEntry{Source: config.RepoCommitted, FilePrecedence: 1},
				owner:   config.RegistryEntry{Source: config.RepoCommitted, FilePrecedence: 2},
			},
			{
				name:    "registry root is not a wildcard for subtrees",
				earlier: config.RegistryEntry{Source: config.UserPersonal, Path: "subtree"},
				owner:   config.RegistryEntry{Source: config.RepoCommitted},
			},
		} {
			t.Run(kind.String()+"/"+tc.name, func(t *testing.T) {
				const url = "git@example.com:shared.git"
				earlier, owner := tc.earlier, tc.owner
				earlier.URL, owner.URL = url, url
				earlier.Ref, owner.Ref = "main", "main"
				earlier.Harnesses, owner.Harnesses = []string{"claude"}, []string{"factory"}
				owner.Collections = ks("bundle")
				cfg := &config.Config{Registries: []config.RegistryEntry{earlier, owner}}
				setCatalogKeys(cfg)
				bundle := ir.NewCollection("bundle", "", "", []ir.CollectionMember{{Name: "member", Kind: kind}}, nil, nil, ir.SourceRef{})
				catalogs := map[string][]catalog.Entry{
					cfg.Registries[0].CatalogKey: {},
					cfg.Registries[1].CatalogKey: {
						{Kind: ir.KindCollection, Name: "bundle", Resource: bundle},
						{Kind: kind, Name: "member", Path: "/cache/shared/member"},
					},
				}
				var local *config.LocalConfig
				if tc.excluded {
					local = &config.LocalConfig{}
					local.SelectorForKindPtr(kind).Exclude = []string{"member"}
				}
				resolved, errs := Resolve(cfg, catalogs, local, WithTargetHarnesses([]hcc.Target{hcc.TargetClaude, hcc.TargetFactory}))
				if len(errs) != 0 {
					t.Fatalf("Resolve errors = %v", errs)
				}
				if len(resolved) != 1 {
					t.Fatalf("resolved = %#v, want exactly one collection member", resolved)
				}
				got := resolved[0]
				if got.Kind != kind || got.Name != "member" || got.Via != "collection:bundle" ||
					got.RegistryURL != url || got.RegistryPath != owner.Path || got.Ref != "main" ||
					got.CatalogKey != cfg.Registries[1].CatalogKey || got.Source != owner.Source ||
					got.FilePrecedence != owner.FilePrecedence || got.Required != owner.Required ||
					!slices.Equal(got.Harnesses, []hcc.Target{hcc.TargetFactory}) {
					t.Fatalf("member lost its collection owner: %#v; want owner %#v with factory targets", got, owner)
				}
			})
		}
	}
}

// spec: resource-selection/same-collection-remains-selected-in-both-install-scopes
func TestResolveSameCollectionRetainsBothScopes(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "machine first", true: "repository first"}[reverse], func(t *testing.T) {
			const url = "git@example.com:shared.git"
			cfg := &config.Config{Registries: []config.RegistryEntry{
				{URL: url, Ref: "main", Source: config.UserPersonal, Collections: ks("bundle")},
				{URL: url, Ref: "main", Source: config.RepoCommitted, Collections: ks("bundle")},
			}}
			if reverse {
				slices.Reverse(cfg.Registries)
			}
			setCatalogKeys(cfg)
			kinds := []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent, ir.KindHook, ir.KindMcpServer}
			members := make([]ir.CollectionMember, 0, len(kinds))
			entries := make([]catalog.Entry, 0, len(kinds)+1)
			for _, kind := range kinds {
				members = append(members, ir.CollectionMember{Name: "shared", Kind: kind})
				entries = append(entries, catalog.Entry{Kind: kind, Name: "shared"})
			}
			bundle := ir.NewCollection("bundle", "", "", members, nil, nil, ir.SourceRef{})
			entries = append(entries, catalog.Entry{Kind: ir.KindCollection, Name: "bundle", Resource: bundle})
			catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: entries}
			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 0 || len(resolved) != 10 {
				t.Fatalf("Resolve = %#v, %v; want five members in each scope", resolved, errs)
			}
			for _, kind := range kinds {
				var sources []config.Source
				for _, member := range resolved {
					if member.Name != "shared" || member.Via != "collection:bundle" {
						t.Fatalf("unexpected member: %#v", member)
					}
					if member.Kind == kind {
						sources = append(sources, member.Source)
					}
				}
				slices.Sort(sources)
				if !slices.Equal(sources, []config.Source{config.UserPersonal, config.RepoCommitted}) {
					t.Fatalf("%s sources = %v, want independent user and repository members", kind, sources)
				}
			}
		})
	}
}

// spec: resource-selection/unqualified-required-member-retains-required-policy
func TestResolveUnqualifiedRequiredCollectionMemberMiss(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: monorepoURL, Ref: "main", Source: config.UserPersonal},
		{URL: monorepoURL, Ref: "main", Source: config.Organization, Required: true, Collections: ks("bundle")},
	}}
	setCatalogKeys(cfg)
	bundle := ir.NewCollection("bundle", "", "", []ir.CollectionMember{
		{Name: "missing", Kind: ir.KindSkill},
		{Name: "present", Kind: ir.KindSkill},
	}, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		{Kind: ir.KindCollection, Name: "bundle", Resource: bundle},
		{Kind: ir.KindSkill, Name: "present"},
	}}
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "malformed"}[malformed], func(t *testing.T) {
			var opts []ResolveOption
			if malformed {
				opts = append(opts, WithMalformed(map[string][]skill.MalformedSkill{cfg.Registries[0].CatalogKey: {
					{DirName: "missing", Path: "/cache/shared/skills/missing", Error: errors.New("invalid skill")},
				}}))
			}
			resolved, errs := Resolve(cfg, catalogs, nil, opts...)
			present := findResolved(resolved, "present")
			if present == nil || !present.Required || present.Source != config.Organization {
				t.Fatalf("positive member lost required organization owner: %#v", resolved)
			}
			if len(errs) != 1 {
				t.Fatalf("errors = %v, want one required member failure", errs)
			}
			if malformed {
				var failure *SelectedMalformedError
				if !errors.As(errs[0], &failure) || !failure.Required || failure.Via != "collection:bundle" || IsSoftSelectedMalformed(errs[0]) {
					t.Fatalf("malformed member lost required policy: %v", errs)
				}
			} else if miss := findSNF(errs, "missing"); miss == nil || !miss.Required || miss.Via != "collection:bundle" {
				t.Fatalf("missing member lost required policy: %v", errs)
			}
		})
	}
}

// spec: resource-selection/unqualified-required-member-retains-required-policy
func TestResolveUnqualifiedNestedMemberRetainsDefinitionOwner(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: monorepoURL, Ref: "main", Source: config.Organization, Harnesses: []string{"claude"}},
		{URL: monorepoURL, Ref: "main", Source: config.UserPersonal, Required: true, Skills: ks("direct"), Harnesses: []string{"factory"}},
		{URL: collectionsHost, Ref: "main", Source: config.RepoCommitted, Collections: ks("outer")},
	}}
	setCatalogKeys(cfg)
	nested := ir.NewCollection("nested", "", "", []ir.CollectionMember{{Name: "member", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
	outer := ir.NewCollection("outer", "", "", nil, []string{"nested"}, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindCollection, Name: "nested", Resource: nested},
			{Kind: ir.KindSkill, Name: "direct"},
			{Kind: ir.KindSkill, Name: "member"},
		},
		cfg.Registries[2].CatalogKey: {{Kind: ir.KindCollection, Name: "outer", Resource: outer}},
	}
	resolved, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses([]hcc.Target{hcc.TargetClaude, hcc.TargetFactory}))
	if len(errs) != 0 || len(resolved) != 2 {
		t.Fatalf("Resolve = %#v, %v; want direct resource and nested member", resolved, errs)
	}
	member := findResolved(resolved, "member")
	if member == nil || member.Source != config.UserPersonal || !member.Required ||
		member.Via != "collection:outer > collection:nested" ||
		!slices.Equal(member.Harnesses, []hcc.Target{hcc.TargetFactory}) {
		t.Fatalf("nested member lost chosen definition owner: %#v", member)
	}
}

// spec: resource-selection/nested-organization-member-retains-harness-diagnostics
func TestResolveUnqualifiedNestedMemberKeepsIncompatibleOrganizationPolicy(t *testing.T) {
	for _, required := range []bool{false, true} {
		t.Run(map[bool]string{false: "default warning", true: "required error"}[required], func(t *testing.T) {
			cfg := &config.Config{Registries: []config.RegistryEntry{
				{URL: monorepoURL, Ref: "main", Source: config.Organization, Required: required, Default: !required, Skills: ks("direct"), Harnesses: []string{"factory"}},
				{URL: collectionsHost, Ref: "main", Source: config.RepoCommitted, Collections: ks("outer"), Harnesses: []string{"claude"}},
			}}
			setCatalogKeys(cfg)
			nested := ir.NewCollection("nested", "", "", []ir.CollectionMember{{Name: "member", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
			outer := ir.NewCollection("outer", "", "", nil, []string{"nested"}, nil, ir.SourceRef{})
			catalogs := map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {
					{Kind: ir.KindCollection, Name: "nested", Resource: nested},
					{Kind: ir.KindSkill, Name: "direct"},
					{Kind: ir.KindSkill, Name: "member"},
				},
				cfg.Registries[1].CatalogKey: {{Kind: ir.KindCollection, Name: "outer", Resource: outer}},
			}
			resolved, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses([]hcc.Target{hcc.TargetClaude, hcc.TargetFactory}))
			if len(resolved) != 1 || resolved[0].Name != "direct" || len(errs) != 1 {
				t.Fatalf("Resolve = %#v, %v; want direct resource and incompatible nested member diagnostic", resolved, errs)
			}
			if required {
				var incompatible *IncompatibleOrganizationRequirement
				if !errors.As(errs[0], &incompatible) || incompatible.Kind != ir.KindSkill || incompatible.Name != "member" || IsWarning(errs[0]) {
					t.Fatalf("nested required member lost hard organization diagnostic: %v", errs)
				}
			} else {
				var incompatible *IncompatibleOrganizationDefault
				if !errors.As(errs[0], &incompatible) || incompatible.Kind != ir.KindSkill || incompatible.Name != "member" || !IsWarning(errs[0]) {
					t.Fatalf("nested default member lost organization warning: %v", errs)
				}
			}
		})
	}
}
