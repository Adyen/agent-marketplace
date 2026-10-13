package resolver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	requiresMPEndpoint = "https://market.example"
	requiresParentDig  = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	requiresHelperDig  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type recordingMarketplaceLookup struct {
	staticMarketplaceResolver
	queries []MarketplaceRequirementQuery
	result  MarketplaceRequirementResult
	err     error
}

func (r *recordingMarketplaceLookup) LookupMarketplaceRequirement(_ context.Context, q MarketplaceRequirementQuery) (MarketplaceRequirementResult, error) {
	r.queries = append(r.queries, q)
	return r.result, r.err
}

// marketplaceRequiresFixture builds a Marketplace entry whose parent pin is a
// direct selection requiring "skill:helper", plus the resolved artifacts the
// fake Marketplace resolver returns. held=true adds a requires-held helper
// pin and artifact.
func marketplaceRequiresFixture(expand bool, requires []any, held bool) (*config.Config, []ResolvedArtifact) {
	parentPin := config.MarketplaceResourcePin{
		RegistryID: "src", RegistrySlug: "tools", Kind: "skill", Name: "parent",
		Digest: requiresParentDig, RegistryCommit: strings.Repeat("c", 40), Direct: true,
	}
	entry := config.MarketplaceEntry{
		URL: requiresMPEndpoint, InstanceID: "mp", Source: config.RepoCommitted,
		Resources: []config.MarketplaceResourcePin{parentPin},
	}
	parentArt := ResolvedArtifact{
		Entry:                   catalog.Entry{Kind: ir.KindSkill, Name: "parent", Resource: requiringSkill("parent", requires...)},
		Source:                  config.RepoCommitted,
		IsMarketplace:           true,
		MarketplaceURL:          requiresMPEndpoint,
		MarketplaceInstanceID:   "mp",
		MarketplaceRegistryID:   "src",
		MarketplaceResourceName: "parent",
		MarketplaceDigest:       requiresParentDig,
		CatalogKey:              config.MarketplaceCatalogKey(entry, parentPin),
	}
	artifacts := []ResolvedArtifact{parentArt}
	if held {
		heldPin := config.MarketplaceResourcePin{
			RegistryID: "src", RegistrySlug: "tools", Kind: "skill", Name: "helper",
			Digest: requiresHelperDig,
			RequiresEdges: config.MarketplaceRequirementEdgesFrom([]config.MarketplaceRequirementEdge{
				{RegistryID: "src", Kind: "skill", Name: "parent", Revision: requiresParentDig},
			}),
		}
		entry.Resources = append(entry.Resources, heldPin)
		artifacts = append(artifacts, ResolvedArtifact{
			Entry:                   catalog.Entry{Kind: ir.KindSkill, Name: "helper", Resource: requiringSkill("helper")},
			Source:                  config.RepoCommitted,
			IsMarketplace:           true,
			MarketplaceURL:          requiresMPEndpoint,
			MarketplaceInstanceID:   "mp",
			MarketplaceRegistryID:   "src",
			MarketplaceResourceName: "helper",
			MarketplaceDigest:       requiresHelperDig,
			CatalogKey:              config.MarketplaceCatalogKey(entry, heldPin),
		})
	}
	cfg := &config.Config{
		Requires:     &config.RequiresConfig{Expand: &expand},
		Marketplaces: []config.MarketplaceEntry{entry},
	}
	return cfg, artifacts
}

func findUnmet(errs []error) *UnmetRequirementWarning {
	for _, err := range errs {
		var unmet *UnmetRequirementWarning
		if errors.As(err, &unmet) {
			return unmet
		}
	}
	return nil
}

// spec: resource-prerequisites/enabled-expansion-selects-a-requires-held-pin-as-implied
// spec: resource-prerequisites/requires-provenance-is-not-collection-provenance
func TestResolveRequiresHeldPinAdmitsAsImplied(t *testing.T) {
	cfg, artifacts := marketplaceRequiresFixture(true, []any{"skill:helper"}, true)
	mp := &recordingMarketplaceLookup{staticMarketplaceResolver: staticMarketplaceResolver{artifacts: artifacts}}

	resolved, errs := Resolve(cfg, nil, nil, WithMarketplaceResolver(mp))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved = %#v, want parent plus implied helper", resolvedNames(resolved))
	}
	var helper *ResolvedArtifact
	for i := range resolved {
		if resolved[i].Name == "helper" {
			helper = &resolved[i]
		}
	}
	if helper == nil || !helper.Implied {
		t.Fatalf("requires-held helper must be selected as implied, got %#v", resolved)
	}
	if len(helper.RequiredBy) != 1 || helper.RequiredBy[0] != (ResourceRef{Kind: ir.KindSkill, Name: "parent"}) {
		t.Fatalf("helper RequiredBy = %#v, want [skill parent]", helper.RequiredBy)
	}
	if helper.Via != "" {
		t.Fatalf("prerequisite provenance must not use collection Via, got %q", helper.Via)
	}
	if len(mp.queries) != 0 {
		t.Fatalf("requires-held match must not consult Marketplace lookup, got %d queries", len(mp.queries))
	}
}

// spec: resource-prerequisites/disabled-expansion-previews-a-requires-held-pin
func TestResolveRequiresHeldPinPreviewsWhenDisabled(t *testing.T) {
	cfg, artifacts := marketplaceRequiresFixture(false, []any{"skill:helper"}, true)
	mp := staticMarketplaceResolver{artifacts: artifacts}
	var preview []ResolvedArtifact

	resolved, _ := Resolve(cfg, nil, nil, WithMarketplaceResolver(mp), WithImpliedPreview(&preview))

	if len(resolved) != 1 || resolved[0].Name != "parent" {
		t.Fatalf("disabled expansion must not select the held pin, got %#v", resolvedNames(resolved))
	}
	if len(preview) != 1 || preview[0].Name != "helper" {
		t.Fatalf("preview = %#v, want [helper]", resolvedNames(preview))
	}
}

// spec: resource-prerequisites/orphaned-requires-held-pin-is-ignored
func TestResolveOrphanedRequiresHeldPinIgnored(t *testing.T) {
	// The selected parent no longer declares the prerequisite.
	cfg, artifacts := marketplaceRequiresFixture(true, nil, true)
	mp := staticMarketplaceResolver{artifacts: artifacts}
	var preview []ResolvedArtifact

	resolved, errs := Resolve(cfg, nil, nil, WithMarketplaceResolver(mp), WithImpliedPreview(&preview))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
	if len(resolved) != 1 || resolved[0].Name != "parent" {
		t.Fatalf("orphaned requires-held pin must not be selected, got %#v", resolvedNames(resolved))
	}
	if len(preview) != 0 {
		t.Fatalf("orphaned requires-held pin must not be previewed, got %#v", resolvedNames(preview))
	}
}

// spec: resource-prerequisites/unbound-marketplace-registry-hint-has-no-fallback
func TestResolveMarketplaceRequirementUnboundHint(t *testing.T) {
	reqs := []any{map[string]any{"name": "helper", "kind": "skill", "registry": "unbound"}}
	cfg, artifacts := marketplaceRequiresFixture(true, reqs, false)
	mp := &recordingMarketplaceLookup{staticMarketplaceResolver: staticMarketplaceResolver{artifacts: artifacts}}

	resolved, errs := Resolve(cfg, nil, nil, WithMarketplaceResolver(mp))

	if len(resolved) != 1 {
		t.Fatalf("resolved = %#v, want only parent", resolvedNames(resolved))
	}
	unmet := findUnmet(errs)
	if unmet == nil || unmet.Reason != UnmetReasonSourceNotConfigured {
		t.Fatalf("unmet = %v, want one source-not-configured warning", errs)
	}
	if len(mp.queries) != 0 {
		t.Fatalf("unbound hint must not issue a Marketplace request, got %d queries", len(mp.queries))
	}
}

// spec: resource-prerequisites/unhinted-marketplace-prerequisite-uses-the-requiring-registry
// spec: resource-prerequisites/enabled-expansion-reports-an-unrecorded-candidate
// spec: resource-prerequisites/unrecordable-marketplace-candidate-has-unrecorded-reason
func TestResolveMarketplaceRequirementUsesRequiringRegistry(t *testing.T) {
	cfg, artifacts := marketplaceRequiresFixture(true, []any{"skill:helper"}, false)
	mp := &recordingMarketplaceLookup{
		staticMarketplaceResolver: staticMarketplaceResolver{artifacts: artifacts},
		result:                    MarketplaceRequirementResult{Found: true, ContentDigest: requiresHelperDig},
	}
	var preview []ResolvedArtifact

	resolved, errs := Resolve(cfg, nil, nil, WithMarketplaceResolver(mp), WithImpliedPreview(&preview))

	if len(resolved) != 1 || resolved[0].Name != "parent" {
		t.Fatalf("lookup candidate is preview-only, resolved = %#v", resolvedNames(resolved))
	}
	if len(mp.queries) != 1 {
		t.Fatalf("queries = %d, want exactly one lookup", len(mp.queries))
	}
	q := mp.queries[0]
	if q.RegistryID != "src" || !q.SameRegistry {
		t.Fatalf("unhinted lookup must target the requiring registry, got %+v", q)
	}
	if q.RegistryCommit != strings.Repeat("c", 40) {
		t.Fatalf("lookup must carry the requiring pin's registry_commit, got %q", q.RegistryCommit)
	}
	if q.Kind != "skill" || q.Name != "helper" {
		t.Fatalf("lookup coordinate = %s/%s, want skill/helper", q.Kind, q.Name)
	}
	unmet := findUnmet(errs)
	if unmet == nil || unmet.Reason != UnmetReasonUnrecorded {
		t.Fatalf("unmet = %v, want one unrecorded warning", errs)
	}
}

// A registry hint naming a different bound registry must not inherit the
// requiring pin's registry_commit: the target registry's current revision
// policy applies instead.
func TestResolveMarketplaceRequirementCrossRegistryHintIgnoresRequirerCommit(t *testing.T) {
	reqs := []any{map[string]any{"name": "helper", "kind": "skill", "registry": "extras"}}
	cfg, artifacts := marketplaceRequiresFixture(true, reqs, false)
	otherPin := config.MarketplaceResourcePin{
		RegistryID: "other", RegistrySlug: "extras", Kind: "skill", Name: "bres",
		Digest: requiresHelperDig, Direct: true,
	}
	cfg.Marketplaces[0].Resources = append(cfg.Marketplaces[0].Resources, otherPin)
	artifacts = append(artifacts, ResolvedArtifact{
		Entry:                   catalog.Entry{Kind: ir.KindSkill, Name: "bres", Resource: requiringSkill("bres")},
		Source:                  config.RepoCommitted,
		IsMarketplace:           true,
		MarketplaceURL:          requiresMPEndpoint,
		MarketplaceInstanceID:   "mp",
		MarketplaceRegistryID:   "other",
		MarketplaceResourceName: "bres",
		MarketplaceDigest:       requiresHelperDig,
		CatalogKey:              config.MarketplaceCatalogKey(cfg.Marketplaces[0], otherPin),
	})
	mp := &recordingMarketplaceLookup{staticMarketplaceResolver: staticMarketplaceResolver{artifacts: artifacts}}

	_, _ = Resolve(cfg, nil, nil, WithMarketplaceResolver(mp))

	if len(mp.queries) != 1 {
		t.Fatalf("queries = %d, want exactly one lookup", len(mp.queries))
	}
	q := mp.queries[0]
	if q.RegistryID != "other" || q.RegistrySlug != "extras" {
		t.Fatalf("lookup must target the hinted registry, got %+v", q)
	}
	if q.SameRegistry {
		t.Fatalf("cross-registry lookup must not be marked same-registry, got %+v", q)
	}
	if q.RegistryCommit != "" {
		t.Fatalf("cross-registry lookup must not carry the requirer's registry_commit, got %q", q.RegistryCommit)
	}
}

// spec: resource-prerequisites/lookup-candidate-is-previewed-not-recorded
func TestResolveMarketplaceRequirementFoundIsPreviewOnly(t *testing.T) {
	cfg, artifacts := marketplaceRequiresFixture(false, []any{"skill:helper"}, false)
	mp := &recordingMarketplaceLookup{
		staticMarketplaceResolver: staticMarketplaceResolver{artifacts: artifacts},
		result:                    MarketplaceRequirementResult{Found: true, ContentDigest: requiresHelperDig},
	}
	var preview []ResolvedArtifact

	resolved, _ := Resolve(cfg, nil, nil, WithMarketplaceResolver(mp), WithImpliedPreview(&preview))

	if len(resolved) != 1 || resolved[0].Name != "parent" {
		t.Fatalf("preview candidate must not join resolved, got %#v", resolvedNames(resolved))
	}
	if len(preview) != 1 || preview[0].Name != "helper" || !preview[0].PreviewOnly {
		t.Fatalf("preview = %#v, want one preview-only helper", preview)
	}
	if preview[0].MarketplaceDigest != requiresHelperDig || preview[0].MarketplaceInstanceID != "mp" {
		t.Fatalf("preview candidate must carry the looked-up identity, got %#v", preview[0])
	}
}

// spec: resource-prerequisites/offline-resolution-makes-no-lookup-request
func TestResolveMarketplaceRequirementOffline(t *testing.T) {
	cfg, artifacts := marketplaceRequiresFixture(true, []any{"skill:helper"}, false)
	mp := &recordingMarketplaceLookup{staticMarketplaceResolver: staticMarketplaceResolver{artifacts: artifacts}}

	resolved, errs := Resolve(cfg, nil, nil, WithMarketplaceResolver(mp), WithMarketplaceResolveMode(MarketplaceResolveModeCachedNoFetch))

	if len(resolved) != 1 {
		t.Fatalf("resolved = %#v, want only parent", resolvedNames(resolved))
	}
	if len(mp.queries) != 0 {
		t.Fatalf("offline resolution must not request the Marketplace, got %d queries", len(mp.queries))
	}
	unmet := findUnmet(errs)
	if unmet == nil || unmet.Reason != UnmetReasonNotFound {
		t.Fatalf("unmet = %v, want one not-found warning", errs)
	}
}
