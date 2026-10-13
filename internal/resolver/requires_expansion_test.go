package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func requiringSkill(name string, requires ...any) ir.Resource {
	return ir.NewSkill(
		name,
		"Requires test knowledge",
		"",
		nil,
		map[string]any{"metadata": map[string]any{"requires": requires}},
		ir.SourceRef{},
	)
}

func requiresConfig(expand bool, skills ...string) *config.Config {
	cfg := &config.Config{
		Requires: &config.RequiresConfig{Expand: &expand},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:skills.git",
			Ref:    "main",
			Skills: ks(skills...),
		}},
	}
	setCatalogKeys(cfg)
	return cfg
}

func requiresCatalogs(entries ...catalog.Entry) map[string][]catalog.Entry {
	return map[string][]catalog.Entry{ck("git@example.com:skills.git", "main"): entries}
}

// spec: resource-prerequisites/disabled-expansion-preserves-selection-while-previewing-prerequisites
func TestResolveDisabledExpansionPreviewsWithoutSelecting(t *testing.T) {
	cfg := requiresConfig(false, "parent")
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)

	var preview []ResolvedArtifact
	resolved, errs := Resolve(cfg, catalogs, nil, WithImpliedPreview(&preview))

	if len(resolved) != 1 || resolved[0].Name != "parent" {
		t.Fatalf("disabled expansion must keep selection unchanged, got %#v", resolvedNames(resolved))
	}
	var unmet *UnmetRequirementWarning
	for _, err := range errs {
		if errors.As(err, &unmet) {
			break
		}
	}
	if unmet == nil || unmet.Requirement.Name != "sibling" || unmet.Reason != UnmetReasonNotFound {
		t.Fatalf("unmet warning = %v, want one not-found warning for skill:sibling", errs)
	}
	if len(preview) != 1 || preview[0].Name != "sibling" {
		t.Fatalf("preview = %#v, want [sibling]", resolvedNames(preview))
	}
	if len(preview[0].RequiredBy) != 1 || preview[0].RequiredBy[0] != (ResourceRef{Kind: ir.KindSkill, Name: "parent"}) {
		t.Fatalf("preview RequiredBy = %#v, want [skill parent]", preview[0].RequiredBy)
	}
	if preview[0].Via != "" {
		t.Fatalf("prerequisite provenance must not use collection Via, got %q", preview[0].Via)
	}
}

// spec: resource-prerequisites/advisory-preview-is-idempotent
func TestResolveDisabledExpansionPreviewIdempotent(t *testing.T) {
	cfg := requiresConfig(false, "parent")
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling", "skill:sibling")},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)

	var first, second []ResolvedArtifact
	resolvedFirst, _ := Resolve(cfg, catalogs, nil, WithImpliedPreview(&first))
	resolvedSecond, _ := Resolve(cfg, catalogs, nil, WithImpliedPreview(&second))

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("preview must dedupe repeated declarations, got %#v and %#v", resolvedNames(first), resolvedNames(second))
	}
	if len(resolvedFirst) != len(resolvedSecond) {
		t.Fatalf("repeated resolution changed selections: %d vs %d", len(resolvedFirst), len(resolvedSecond))
	}
}

// spec: resource-prerequisites/transitive-prerequisites-are-selected-after-collections
func TestResolveEnabledExpansionSelectsTransitiveClosure(t *testing.T) {
	cfg := requiresConfig(true, "parent")
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:middle")},
		catalog.Entry{Kind: ir.KindSkill, Name: "middle", Resource: requiringSkill("middle", "skill:leaf")},
		catalog.Entry{Kind: ir.KindSkill, Name: "leaf"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	names := resolvedNames(resolved)
	for _, want := range []string{"parent", "middle", "leaf"} {
		if !names[want] {
			t.Fatalf("expected %q in resolved set %v", want, names)
		}
	}
	byName := map[string]ResolvedArtifact{}
	for _, a := range resolved {
		byName[a.Name] = a
	}
	if byName["middle"].Required {
		t.Fatal("implied resource must not become a required selection")
	}
	if len(byName["middle"].RequiredBy) != 1 || byName["middle"].RequiredBy[0].Name != "parent" {
		t.Fatalf("middle RequiredBy = %#v", byName["middle"].RequiredBy)
	}
	if len(byName["leaf"].RequiredBy) != 1 || byName["leaf"].RequiredBy[0].Name != "middle" {
		t.Fatalf("leaf RequiredBy = %#v", byName["leaf"].RequiredBy)
	}
}

// spec: resource-prerequisites/missing-candidate-has-not-found-reason
func TestResolveEnabledExpansionStopsAtMissingPrerequisite(t *testing.T) {
	cfg := requiresConfig(true, "parent")
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:missing")},
		catalog.Entry{Kind: ir.KindSkill, Name: "deep"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if resolvedNames(resolved)["missing"] || resolvedNames(resolved)["deep"] {
		t.Fatalf("missing prerequisite must not be selected or traversed: %v", resolvedNames(resolved))
	}
	var unmet []*UnmetRequirementWarning
	for _, err := range errs {
		var w *UnmetRequirementWarning
		if errors.As(err, &w) {
			unmet = append(unmet, w)
		}
	}
	if len(unmet) != 1 || unmet[0].Requirement.Name != "missing" {
		t.Fatalf("unmet warnings = %v, want exactly one for skill:missing", errs)
	}
	if unmet[0].Reason != UnmetReasonNotFound {
		t.Fatalf("unmet reason = %q, want %q", unmet[0].Reason, UnmetReasonNotFound)
	}
}

// spec: resource-prerequisites/install-finds-an-unmet-prerequisite
func TestResolveDisabledExpansionWarnsOnlyForUnmet(t *testing.T) {
	cfg := requiresConfig(false, "parent")
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling", "skill:absent")},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)

	var preview []ResolvedArtifact
	resolved, errs := Resolve(cfg, catalogs, nil, WithImpliedPreview(&preview))

	if len(resolved) != 1 {
		t.Fatalf("disabled expansion must not select, got %v", resolvedNames(resolved))
	}
	var unmet []*UnmetRequirementWarning
	for _, err := range errs {
		var w *UnmetRequirementWarning
		if errors.As(err, &w) {
			unmet = append(unmet, w)
		}
	}
	if len(unmet) != 2 {
		t.Fatalf("want unmet warnings for sibling and absent, got %v", errs)
	}
	if len(preview) != 1 || preview[0].Name != "sibling" {
		t.Fatalf("preview = %v, want [sibling]", resolvedNames(preview))
	}
}

// spec: resource-prerequisites/existing-selection-satisfies-by-kind-and-name
func TestResolveEnabledExpansionSatisfiedByOtherRegistry(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
		{URL: "git@example.com:other.git", Ref: "main", Skills: ks("sibling")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		},
		ck("git@example.com:other.git", "main"): {
			{Kind: ir.KindSkill, Name: "sibling"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	if len(resolved) != 2 {
		t.Fatalf("selection by kind+name from any source must satisfy, got %v", resolvedNames(resolved))
	}
}

// spec: resource-prerequisites/existing-selection-satisfies-by-kind-and-name
func TestResolveEnabledExpansionSatisfiedAcrossScopeAndMarketplace(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Source: config.RepoCommitted, Skills: ks("parent")},
		{URL: "git@example.com:skills.git", Ref: "main", Source: config.UserPersonal, Skills: ks("sibling")},
	}}
	setCatalogKeys(cfg)
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		var w *UnmetRequirementWarning
		if errors.As(err, &w) {
			t.Fatalf("selected prerequisite must not warn: %v", w)
		}
	}
	siblings := 0
	for _, a := range resolved {
		if a.Name == "sibling" {
			siblings++
			if a.Source != config.UserPersonal {
				t.Fatalf("the machine-scope selection must satisfy in place, got scope %v", a.Source)
			}
		}
	}
	if siblings != 1 {
		t.Fatalf("global satisfaction admitted a duplicate sibling: %v", resolvedNames(resolved))
	}

	endpoint := "https://market.example"
	mpEntry := config.MarketplaceEntry{URL: endpoint, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{
		{RegistryID: "src", RegistrySlug: "tools", Kind: "skill", Name: "sibling"},
	}}
	mpCfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Source: config.RepoCommitted, Skills: ks("parent")},
	}, Marketplaces: []config.MarketplaceEntry{mpEntry}}
	setCatalogKeys(mpCfg)
	mp := staticMarketplaceResolver{artifacts: []ResolvedArtifact{{
		Entry: catalog.Entry{Kind: ir.KindSkill, Name: "sibling"}, Source: config.RepoCommitted,
		IsMarketplace: true, MarketplaceURL: endpoint, MarketplaceRegistryID: "src",
		CatalogKey: config.MarketplaceCatalogKey(mpEntry, mpEntry.Resources[0]),
	}}}

	resolved, errs = Resolve(mpCfg, catalogs, nil, WithMarketplaceResolver(mp))
	for _, err := range errs {
		var w *UnmetRequirementWarning
		if errors.As(err, &w) {
			t.Fatalf("Marketplace-satisfied prerequisite must not warn: %v", w)
		}
	}
	siblings = 0
	for _, a := range resolved {
		if a.Name == "sibling" {
			siblings++
			if !a.IsMarketplace {
				t.Fatal("the Marketplace selection must satisfy in place")
			}
		}
	}
	if siblings != 1 {
		t.Fatalf("Marketplace satisfaction admitted a duplicate sibling: %v", resolvedNames(resolved))
	}
}

// spec: resource-prerequisites/disabled-expansion-preserves-selection-while-previewing-prerequisites
func TestResolveImpliedPreviewCaptureClearedPerCall(t *testing.T) {
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)
	var preview []ResolvedArtifact
	if _, errs := Resolve(requiresConfig(false, "parent"), catalogs, nil, WithImpliedPreview(&preview)); len(preview) != 1 {
		t.Fatalf("advisory capture = %#v, want one previewed resource (errs %v)", preview, errs)
	}

	if _, errs := Resolve(requiresConfig(true, "parent"), catalogs, nil, WithImpliedPreview(&preview)); len(preview) != 0 {
		t.Fatalf("enabled expansion leaked the stale preview %#v (errs %v)", preview, errs)
	}
	if _, errs := Resolve(requiresConfig(false, "parent"), requiresCatalogs(), nil, WithImpliedPreview(&preview)); len(preview) != 0 {
		t.Fatalf("empty resolution leaked the stale preview %#v (errs %v)", preview, errs)
	}
}

// spec: resource-prerequisites/unhinted-prerequisite-uses-the-requiring-source-root
func TestResolveEnabledExpansionUnhintedUsesRequiringSource(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
		{URL: "git@example.com:other.git", Ref: "main"},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		},
		ck("git@example.com:other.git", "main"): {
			{Kind: ir.KindSkill, Name: "sibling"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if resolvedNames(resolved)["sibling"] {
		t.Fatal("unhinted prerequisite must not be selected from another source identity")
	}
	var unmet *UnmetRequirementWarning
	for _, err := range errs {
		if errors.As(err, &unmet) {
			break
		}
	}
	if unmet == nil || unmet.Reason != UnmetReasonNotFound {
		t.Fatalf("want one not-found unmet warning, got %v", errs)
	}
}

// spec: resource-prerequisites/unconfigured-git-hint-has-no-fallback
// spec: resource-prerequisites/unconfigured-hint-has-source-not-configured-reason
func TestResolveEnabledExpansionUnconfiguredHintNoFallback(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
	}}
	setCatalogKeys(cfg)
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent",
			map[string]any{"kind": "skill", "name": "sibling", "registry": "git@example.com:unconfigured.git"})},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if resolvedNames(resolved)["sibling"] {
		t.Fatal("unconfigured hint must not fall back to the requiring source")
	}
	var unmet *UnmetRequirementWarning
	for _, err := range errs {
		if errors.As(err, &unmet) {
			break
		}
	}
	if unmet == nil {
		t.Fatalf("want unmet warning, got %v", errs)
	}
	if unmet.Reason != UnmetReasonSourceNotConfigured {
		t.Fatalf("unmet reason = %q, want %q", unmet.Reason, UnmetReasonSourceNotConfigured)
	}
}

// spec: resource-prerequisites/configured-git-hint-narrows-lookup-to-its-root
func TestResolveEnabledExpansionConfiguredGitHint(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
		{URL: "git@example.com:hints.git", Ref: "main"},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent",
				map[string]any{"kind": "skill", "name": "sibling", "registry": "git@example.com:hints.git"})},
		},
		ck("git@example.com:hints.git", "main"): {
			{Kind: ir.KindSkill, Name: "sibling"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	if !resolvedNames(resolved)["sibling"] {
		t.Fatalf("configured hint must admit the prerequisite, got %v", resolvedNames(resolved))
	}
	for _, a := range resolved {
		if a.Name == "sibling" && a.RegistryURL != "git@example.com:hints.git" {
			t.Fatalf("sibling came from %q, want the hinted registry", a.RegistryURL)
		}
	}
}

// spec: resource-prerequisites/prerequisite-is-explicitly-excluded
// spec: resource-prerequisites/excluded-candidate-has-excluded-reason
func TestResolveEnabledExpansionExplicitlyExcluded(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: kse([]string{"parent"}, []string{"sibling"})},
	}}
	setCatalogKeys(cfg)
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	names := resolvedNames(resolved)
	if names["sibling"] || !names["parent"] {
		t.Fatalf("excluded prerequisite must stay unmet and keep requirer, got %v", names)
	}
	var unmet *UnmetRequirementWarning
	for _, err := range errs {
		if errors.As(err, &unmet) {
			break
		}
	}
	if unmet == nil || unmet.Reason != UnmetReasonExcluded {
		t.Fatalf("want excluded-reason unmet warning, got %v", errs)
	}
}

// spec: resource-prerequisites/excluded-prerequisite-is-required
func TestResolveEnabledExpansionRequiredEntrySuppliesExcluded(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: kse([]string{"parent"}, []string{"sibling"})},
		{URL: "git@example.com:required.git", Ref: "main", Required: true, Skills: ks("sibling")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		},
		ck("git@example.com:required.git", "main"): {
			{Kind: ir.KindSkill, Name: "sibling"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none; required entry must keep prerequisite selected", errs)
	}
	if !resolvedNames(resolved)["sibling"] {
		t.Fatal("required entry must supply the prerequisite")
	}
}

// spec: resource-prerequisites/cycles-and-competing-implied-candidates-resolve-deterministically
func TestResolveEnabledExpansionCyclesTerminate(t *testing.T) {
	cfg := requiresConfig(true, "alpha")
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "alpha", Resource: requiringSkill("alpha", "skill:beta")},
		catalog.Entry{Kind: ir.KindSkill, Name: "beta", Resource: requiringSkill("beta", "skill:alpha")},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	names := resolvedNames(resolved)
	if !names["alpha"] || !names["beta"] {
		t.Fatalf("cycle must resolve both members, got %v", names)
	}
}

// spec: resource-prerequisites/direct-selection-wins-an-implied-collision
func TestResolveEnabledExpansionDirectSelectionWins(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
		{URL: "git@example.com:direct.git", Ref: "main", Skills: ks("sibling")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
			{Kind: ir.KindSkill, Name: "sibling", Path: "/cache/skills/sibling"},
		},
		ck("git@example.com:direct.git", "main"): {
			{Kind: ir.KindSkill, Name: "sibling", Path: "/cache/direct/sibling"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	count := 0
	for _, a := range resolved {
		if a.Name == "sibling" {
			count++
			if a.RegistryURL != "git@example.com:direct.git" {
				t.Fatalf("direct selection must win, got %q", a.RegistryURL)
			}
		}
	}
	if count != 1 {
		t.Fatalf("want exactly one sibling selection, got %d", count)
	}
}

// spec: resource-prerequisites/collection-member-requirement-retains-both-relationships
func TestResolveEnabledExpansionCollectionMemberRequirement(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Collections: ks("bundle")},
	}}
	setCatalogKeys(cfg)
	collection := ir.NewCollection("bundle", "1.0.0", "bundle", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "member"},
	}, nil, nil, ir.SourceRef{})
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindCollection, Name: "bundle", Resource: collection},
		catalog.Entry{Kind: ir.KindSkill, Name: "member", Resource: requiringSkill("member", "skill:helper")},
		catalog.Entry{Kind: ir.KindSkill, Name: "helper"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	byName := map[string]ResolvedArtifact{}
	for _, a := range resolved {
		byName[a.Name] = a
	}
	member, ok := byName["member"]
	if !ok || member.Via == "" {
		t.Fatalf("collection member must keep collection Via, got %#v", byName)
	}
	helper, ok := byName["helper"]
	if !ok {
		t.Fatalf("implied helper missing from %v", resolvedNames(resolved))
	}
	if helper.Via != "" {
		t.Fatalf("prerequisite provenance must not be collection provenance, Via = %q", helper.Via)
	}
	if len(helper.RequiredBy) != 1 || helper.RequiredBy[0].Name != "member" {
		t.Fatalf("helper RequiredBy = %#v, want [skill member]", helper.RequiredBy)
	}
}

// spec: resource-prerequisites/implied-resources-stay-within-scope-and-effective-targets
func TestResolveEnabledExpansionScopeAndTargets(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
	}}
	setCatalogKeys(cfg)
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	var parentSource config.Source
	for _, a := range resolved {
		if a.Name == "parent" {
			parentSource = a.Source
		}
	}
	for _, a := range resolved {
		if a.Name == "sibling" {
			if a.Required {
				t.Fatal("implied resource must not be a required selection")
			}
			if a.Source != parentSource {
				t.Fatalf("implied resource scope = %v, want requiring scope %v", a.Source, parentSource)
			}
		}
	}
}

// spec: resource-prerequisites/cross-scope-exclusion-does-not-change-machine-selection
func TestResolveEnabledExpansionCrossScopeExclusionKeepsMachineSelection(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Source: config.RepoCommitted, Skills: kse(nil, []string{"sibling"})},
		{URL: "git@example.com:skills.git", Ref: "main", Source: config.UserPersonal, Skills: ks("parent")},
	}}
	setCatalogKeys(cfg)
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:sibling")},
		catalog.Entry{Kind: ir.KindSkill, Name: "sibling"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none; a repo-scope exclusion must not touch machine selection", errs)
	}
	var sibling *ResolvedArtifact
	for i := range resolved {
		if resolved[i].Name == "sibling" {
			sibling = &resolved[i]
		}
	}
	if sibling == nil {
		t.Fatal("machine-scope implied prerequisite missing")
	}
	if sibling.Source != config.UserPersonal {
		t.Fatalf("implied prerequisite scope = %v, want machine scope", sibling.Source)
	}
}

// spec: resource-prerequisites/directory-hints-require-an-absolute-configured-location
func TestResolveEnabledExpansionDirectoryHintRequiresAbsoluteConfiguredLocation(t *testing.T) {
	expand := true
	newCfg := func() *config.Config {
		cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
			{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
			{URL: "/opt/registry", V4Kind: config.V4SourceDirectory},
		}}
		setCatalogKeys(cfg)
		return cfg
	}
	cfg := newCfg()
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent",
				map[string]any{"kind": "skill", "name": "sibling", "registry": "/opt/registry"})},
		},
		cfg.Registries[1].CatalogKey: {
			{Kind: ir.KindSkill, Name: "sibling"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	if !resolvedNames(resolved)["sibling"] {
		t.Fatal("absolute directory hint must match the configured directory source")
	}

	cfg2 := newCfg()
	catalogs2 := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent",
				map[string]any{"kind": "skill", "name": "sibling", "registry": "registry"})},
		},
		cfg2.Registries[1].CatalogKey: {
			{Kind: ir.KindSkill, Name: "sibling"},
		},
	}
	resolved2, errs2 := Resolve(cfg2, catalogs2, nil)
	if resolvedNames(resolved2)["sibling"] {
		t.Fatal("relative directory hint must not match")
	}
	var unmet *UnmetRequirementWarning
	for _, err := range errs2 {
		if errors.As(err, &unmet) {
			break
		}
	}
	if unmet == nil || unmet.Reason != UnmetReasonSourceNotConfigured {
		t.Fatalf("want source-not-configured unmet warning, got %v", errs2)
	}
}

// spec: resource-prerequisites/malformed-candidate-keeps-its-diagnostic-attribution
func TestResolveEnabledExpansionMalformedCandidateRetainsDiagnostic(t *testing.T) {
	cfg := requiresConfig(true, "parent")
	key := cfg.Registries[0].CatalogKey
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "skill:broken")},
	)
	malformed := malformedFixture(key, skill.MalformedSkill{
		DirName: "broken",
		Path:    "/cache/skills/broken",
		Error:   errors.New("yaml: did not find expected ',' or ']'"),
	})

	resolved, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if !resolvedNames(resolved)["parent"] {
		t.Fatalf("requiring resource must remain selected, got %v", resolvedNames(resolved))
	}
	var sme *SelectedMalformedError
	var unmet *UnmetRequirementWarning
	for _, err := range errs {
		if errors.As(err, &sme) {
			continue
		}
		if errors.As(err, &unmet) {
			continue
		}
	}
	if sme == nil || sme.Name != "broken" {
		t.Fatalf("typed malformed diagnostic missing, got %v", errs)
	}
	if unmet == nil || unmet.Reason != UnmetReasonNotFound {
		t.Fatalf("want not-found unmet warning, got %v", errs)
	}
}

// spec: resource-prerequisites/implied-resources-stay-within-scope-and-effective-targets
// spec: resource-prerequisites/advisory-preview-is-idempotent
func TestResolveEnabledExpansionOccurrenceKeyedLookup(t *testing.T) {
	expand := true
	parent := ir.NewSkill(
		"parent", "Requires test knowledge", "", nil,
		map[string]any{"metadata": map[string]any{"requires": []any{
			map[string]any{"kind": "skill", "name": "dep", "registry": "git@example.com:unconfigured.git"},
			"skill:dep",
		}}},
		ir.SourceRef{},
	)
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Source: config.RepoCommitted, Skills: ks("parent")},
		{URL: "git@example.com:machine.git", Ref: "main", Source: config.UserPersonal, Skills: ks("parent")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: parent},
		},
		ck("git@example.com:machine.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: parent},
			{Kind: ir.KindSkill, Name: "dep"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	var dep *ResolvedArtifact
	for i := range resolved {
		if resolved[i].Name == "dep" {
			dep = &resolved[i]
		}
	}
	if dep == nil {
		t.Fatalf("the second occurrence's eligible declaration must still be looked up and admitted: %v", resolvedNames(resolved))
	}
	if dep.Source != config.UserPersonal {
		t.Fatalf("implied dep scope = %v, want machine scope of the eligible occurrence", dep.Source)
	}
	for _, err := range errs {
		var w *UnmetRequirementWarning
		if errors.As(err, &w) {
			t.Fatalf("dep is selected, so no requirement may warn; a reason leaked between occurrences: %v", w)
		}
	}

	cfg.Registries[0].Skills = kse([]string{"parent"}, []string{"dep"})
	resolved, errs = Resolve(cfg, catalogs, nil)
	for _, a := range resolved {
		if a.Name == "dep" && a.Source != config.UserPersonal {
			t.Fatalf("excluded-scope repeat admitted dep at %v", a.Source)
		}
	}
	if !resolvedNames(resolved)["dep"] {
		t.Fatalf("excluded-scope repeat lost the eligible implied dep: %v", resolvedNames(resolved))
	}
	for _, err := range errs {
		var w *UnmetRequirementWarning
		if errors.As(err, &w) {
			t.Fatalf("dep is selected, so no requirement may warn: %v", w)
		}
	}
}

// spec: resource-prerequisites/malformed-candidate-keeps-its-diagnostic-attribution
func TestResolveEnabledExpansionMalformedCandidateAttributesEachRequirer(t *testing.T) {
	cfg := requiresConfig(true, "first", "second")
	key := cfg.Registries[0].CatalogKey
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "first", Resource: requiringSkill("first", "skill:broken")},
		catalog.Entry{Kind: ir.KindSkill, Name: "second", Resource: requiringSkill("second", "skill:broken")},
	)
	malformed := malformedFixture(key, skill.MalformedSkill{
		DirName: "broken",
		Path:    "/cache/skills/broken",
		Error:   errors.New("yaml: did not find expected ',' or ']'"),
	})

	_, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	byRequirer := map[string]*SelectedMalformedError{}
	for _, err := range errs {
		var sme *SelectedMalformedError
		if errors.As(err, &sme) {
			if sme.RequiredBy == nil {
				t.Fatalf("malformed requirement diagnostic must attribute the requiring resource, got %v", sme)
			}
			byRequirer[sme.RequiredBy.Name] = sme
			if !strings.Contains(sme.Error(), "first") && !strings.Contains(sme.Error(), "second") {
				t.Fatalf("formatted diagnostic must name the requirer: %q", sme.Error())
			}
		}
	}
	for _, want := range []string{"first", "second"} {
		sme, ok := byRequirer[want]
		if !ok {
			t.Fatalf("missing attributed malformed diagnostic for requirer %q in %v", want, errs)
		}
		if sme.RequiredBy.Kind != ir.KindSkill || sme.Name != "broken" {
			t.Fatalf("attribution = %#v, want skill %q requiring broken", sme.RequiredBy, want)
		}
		if !strings.HasPrefix(sme.Error(), "skill \""+want+"\"") {
			t.Fatalf("error must prefix the requirer, got %q", sme.Error())
		}
	}
	direct := requiresConfig(true, "broken")
	_, directErrs := Resolve(direct, requiresCatalogs(), nil, WithMalformed(malformed))
	for _, err := range directErrs {
		var sme *SelectedMalformedError
		if errors.As(err, &sme) && sme.RequiredBy != nil {
			t.Fatalf("direct malformed selection gained requirement attribution: %#v", sme.RequiredBy)
		}
	}
}

// spec: resource-prerequisites/configured-git-hint-narrows-lookup-to-its-root
func TestResolveEnabledExpansionNormalizedHintAndMultipleRoots(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
		{URL: "git@example.com:hints", Ref: "main", Path: "libs/core"},
		{URL: "git@example.com:hints", Ref: "dev", Path: "team/shared"},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent",
				map[string]any{"kind": "skill", "name": "dep", "registry": "git@example.com:hints.git/", "path": "team/other/../shared"})},
		},
		ck("git@example.com:hints", "main", "libs/core"): {
			{Kind: ir.KindSkill, Name: "dep", Path: "/cache/main/dep"},
		},
		ck("git@example.com:hints", "dev", "team/shared"): {
			{Kind: ir.KindSkill, Name: "dep", Path: "/cache/dev/dep"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	count := 0
	for _, a := range resolved {
		if a.Name == "dep" {
			count++
			if a.Ref != "dev" || a.RegistryPath != "team/shared" || a.Path != "/cache/dev/dep" {
				t.Fatalf("dep came from ref %q path %q (%q), want the dev root the hint narrowed to", a.Ref, a.RegistryPath, a.Path)
			}
		}
	}
	if count != 1 {
		t.Fatalf("normalized hinted root must admit exactly one dep, got %v", resolvedNames(resolved))
	}
}

// spec: resource-prerequisites/cycles-and-competing-implied-candidates-resolve-deterministically
func TestResolveEnabledExpansionCompetingImpliedFirstWinner(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("alpha", "beta")},
		{URL: "git@example.com:alpha-hints.git", Ref: "main"},
		{URL: "git@example.com:beta-hints.git", Ref: "main"},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:skills.git", "main"): {
			{Kind: ir.KindSkill, Name: "alpha", Resource: requiringSkill("alpha",
				map[string]any{"kind": "skill", "name": "dep", "registry": "git@example.com:alpha-hints.git"})},
			{Kind: ir.KindSkill, Name: "beta", Resource: requiringSkill("beta",
				map[string]any{"kind": "skill", "name": "dep", "registry": "git@example.com:beta-hints.git"})},
		},
		ck("git@example.com:alpha-hints.git", "main"): {
			{Kind: ir.KindSkill, Name: "dep", Path: "/cache/alpha/dep"},
		},
		ck("git@example.com:beta-hints.git", "main"): {
			{Kind: ir.KindSkill, Name: "dep", Path: "/cache/beta/dep"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	var dep *ResolvedArtifact
	count := 0
	for i := range resolved {
		if resolved[i].Name == "dep" {
			dep = &resolved[i]
			count++
		}
	}
	if count != 1 || dep == nil {
		t.Fatalf("competing requirers must admit exactly one implied dep, got %d", count)
	}
	if dep.RegistryURL != "git@example.com:alpha-hints.git" || dep.Path != "/cache/alpha/dep" {
		t.Fatalf("first implied winner came from %q at %q, want the first requirer's hinted source", dep.RegistryURL, dep.Path)
	}
	if len(dep.RequiredBy) != 2 || dep.RequiredBy[0].Name != "alpha" || dep.RequiredBy[1].Name != "beta" {
		t.Fatalf("first implied winner must record both requirers in resolved order, got %#v", dep.RequiredBy)
	}
}

// spec: resource-prerequisites/transitive-prerequisites-are-selected-after-collections
func TestResolveEnabledExpansionCrossKindPrerequisite(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
	}}
	setCatalogKeys(cfg)
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "command:helper")},
		catalog.Entry{Kind: ir.KindCommand, Name: "helper"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	for _, a := range resolved {
		if a.Name == "helper" {
			if a.Kind != ir.KindCommand {
				t.Fatalf("cross-kind prerequisite kind = %v, want command", a.Kind)
			}
			return
		}
	}
	t.Fatalf("cross-kind prerequisite missing from %v", resolvedNames(resolved))
}

// spec: resource-prerequisites/excluded-candidate-has-excluded-reason
func TestResolveEnabledExpansionExcludeAllSuppressesCandidate(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
		{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent"), Commands: config.KindSelector{ExcludeAll: true}},
	}}
	setCatalogKeys(cfg)
	catalogs := requiresCatalogs(
		catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", "command:dep")},
		catalog.Entry{Kind: ir.KindCommand, Name: "dep"},
	)

	resolved, errs := Resolve(cfg, catalogs, nil)
	if resolvedNames(resolved)["dep"] {
		t.Fatal("exclude-all must prevent implied admission")
	}
	var unmet *UnmetRequirementWarning
	for _, err := range errs {
		if errors.As(err, &unmet) {
			break
		}
	}
	if unmet == nil || unmet.Reason != UnmetReasonExcluded {
		t.Fatalf("want excluded-reason unmet warning, got %v", errs)
	}
}

// spec: resource-prerequisites/implied-resources-stay-within-scope-and-effective-targets
func TestResolveEnabledExpansionImpliedTargetsIntersectEffectiveSet(t *testing.T) {
	expand := true
	newCfg := func(harnesses []string) *config.Config {
		cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}, Registries: []config.RegistryEntry{
			{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("parent")},
			{URL: "git@example.com:supply.git", Ref: "main", Harnesses: harnesses},
		}}
		setCatalogKeys(cfg)
		return cfg
	}
	catalogs := func() map[string][]catalog.Entry {
		return map[string][]catalog.Entry{
			ck("git@example.com:skills.git", "main"): {
				{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent",
					map[string]any{"kind": "skill", "name": "dep", "registry": "git@example.com:supply.git"})},
			},
			ck("git@example.com:supply.git", "main"): {
				{Kind: ir.KindSkill, Name: "dep"},
			},
		}
	}
	targets := []hcc.Target{hcc.TargetDevin}

	resolved, errs := Resolve(newCfg([]string{"claude"}), catalogs(), nil, WithTargetHarnesses(targets))
	if resolvedNames(resolved)["dep"] {
		t.Fatal("explicit-empty intersection must not admit the prerequisite")
	}
	var unmet *UnmetRequirementWarning
	for _, err := range errs {
		if errors.As(err, &unmet) {
			break
		}
	}
	if unmet == nil || unmet.Reason != UnmetReasonNotFound {
		t.Fatalf("want not-found unmet warning for empty target intersection, got %v", errs)
	}

	resolved, errs = Resolve(newCfg([]string{}), catalogs(), nil, WithTargetHarnesses([]hcc.Target{hcc.TargetDevin, hcc.TargetClaude}))
	if resolvedNames(resolved)["dep"] {
		t.Fatal("explicit-empty supplier harness list must not admit the prerequisite")
	}
	unmet = nil
	for _, err := range errs {
		if errors.As(err, &unmet) {
			break
		}
	}
	if unmet == nil || unmet.Reason != UnmetReasonNotFound {
		t.Fatalf("want not-found unmet warning for explicit-empty harness list, got %v", errs)
	}

	resolved, errs = Resolve(newCfg([]string{"claude", "devin"}), catalogs(), nil, WithTargetHarnesses(targets))
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}
	for _, a := range resolved {
		if a.Name == "dep" {
			if len(a.Harnesses) != 1 || a.Harnesses[0] != hcc.TargetDevin {
				t.Fatalf("implied dep harnesses = %v, want [devin]", a.Harnesses)
			}
			return
		}
	}
	t.Fatalf("intersected prerequisite missing from %v", resolvedNames(resolved))
}
