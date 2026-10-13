package resolver

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

type ResourceRef struct {
	Kind ir.ResourceKind
	Name string
}

type UnmetRequirementReason string

const (
	UnmetReasonNotFound            UnmetRequirementReason = "not-found"
	UnmetReasonExcluded            UnmetRequirementReason = "excluded"
	UnmetReasonSourceNotConfigured UnmetRequirementReason = "source-not-configured"
	UnmetReasonUnrecorded          UnmetRequirementReason = "unrecorded"
)

// MarketplaceRequirementQuery asks a configured Marketplace resolver whether
// one prerequisite declaration resolves to a resource inside the requiring
// pin's configured Marketplace entry. A found candidate is never recorded,
// so the result is an identity, not an artifact.
type MarketplaceRequirementQuery struct {
	Entry          config.MarketplaceEntry
	RegistryID     string
	RegistrySlug   string
	SameRegistry   bool
	RegistryCommit string // requirer pin registry_commit; honored only when SameRegistry
	Kind           string // Marketplace wire kind
	Name           string
	Path           string // trimmed path hint; "" when absent
}

// MarketplaceRequirementResult reports whether the query resolved.
type MarketplaceRequirementResult struct {
	Found         bool
	ContentDigest string
}

// MarketplaceRequirementLookup is an optional extension a Marketplace resolver
// can implement to answer prerequisite lookups. Implementations without it
// behave as not found.
type MarketplaceRequirementLookup interface {
	LookupMarketplaceRequirement(ctx context.Context, q MarketplaceRequirementQuery) (MarketplaceRequirementResult, error)
}

func WithImpliedPreview(preview *[]ResolvedArtifact) ResolveOption {
	return func(opts *resolveOptions) {
		opts.impliedPreview = preview
	}
}

type unmetKey struct {
	machine    bool
	catalogKey string
	requirer   resourceSelection
	required   resourceSelection
	registry   string
	path       string
}

func newUnmetKey(requirer ResolvedArtifact, req catalog.Requirement) unmetKey {
	return unmetKey{
		machine:    requirer.Source.IsMachineScope(),
		catalogKey: requirer.CatalogKey,
		requirer:   resourceSelection{kind: requirer.Kind, name: requirer.Name},
		required:   resourceSelection{kind: req.Kind, name: strings.TrimSpace(req.Name)},
		registry:   strings.TrimSpace(req.Registry),
		path:       normalizedEntryPath(req.Path),
	}
}

type requirementCandidate struct {
	entry   config.RegistryEntry
	catalog catalog.Entry
}

type catalogIndexes map[string]map[resourceSelection]catalog.Entry

func (ix catalogIndexes) index(catalogKey string, entries []catalog.Entry) map[resourceSelection]catalog.Entry {
	built, ok := ix[catalogKey]
	if !ok {
		built = indexCatalog(entries)
		ix[catalogKey] = built
	}
	return built
}

func requirementCandidateEntries(requirer ResolvedArtifact, req catalog.Requirement, cfg *config.Config) ([]config.RegistryEntry, bool) {
	hint := strings.TrimSpace(req.Registry)
	if hint == "" {
		var entries []config.RegistryEntry
		for _, entry := range cfg.Registries {
			if entry.CatalogKey == requirer.CatalogKey {
				entries = append(entries, entry)
			}
		}
		return entries, false
	}

	matched := false
	var entries []config.RegistryEntry
	for _, entry := range cfg.Registries {
		if entry.IsLocal() {
			if !filepath.IsAbs(hint) {
				continue
			}
			local, err := entry.ResolveLocalPath("")
			if err != nil || local == "" {
				continue
			}
			if filepath.Clean(local) != filepath.Clean(hint) {
				continue
			}
			matched = true
			entries = append(entries, entry)
			continue
		}
		if config.NormalizeGitURL(entry.URL) != config.NormalizeGitURL(hint) {
			continue
		}
		matched = true
		if pathHint := strings.TrimSpace(req.Path); pathHint != "" {
			if normalizedEntryPath(entry.Path) != normalizedEntryPath(pathHint) {
				continue
			}
		}
		entries = append(entries, entry)
	}
	return entries, !matched
}

func requirementCandidates(requirer ResolvedArtifact, req catalog.Requirement, cfg *config.Config, catalogs map[string][]catalog.Entry, indexes catalogIndexes) (candidates []requirementCandidate, sourceMissing bool) {
	entries, missing := requirementCandidateEntries(requirer, req, cfg)
	if missing {
		return nil, true
	}
	sel := resourceSelection{kind: req.Kind, name: strings.TrimSpace(req.Name)}
	for _, entry := range entries {
		entriesInCatalog, ok := catalogs[entry.CatalogKey]
		if !ok {
			continue
		}
		candidate, found := indexes.index(entry.CatalogKey, entriesInCatalog)[sel]
		if !found {
			continue
		}
		candidates = append(candidates, requirementCandidate{entry: entry, catalog: candidate})
	}
	return candidates, false
}

func impliedArtifact(cand requirementCandidate, requirer ResolvedArtifact, resources map[string]map[string]ir.Resource, targetSet []hcc.Target) ResolvedArtifact {
	return ResolvedArtifact{
		Entry:          attachResource(cand.catalog, resources, cand.entry.CatalogKey),
		RegistryURL:    cand.entry.URL,
		RegistryPath:   normalizedEntryPath(cand.entry.Path),
		Ref:            cand.entry.Ref,
		V4Kind:         cand.entry.V4Kind,
		Source:         requirer.Source,
		FilePrecedence: cand.entry.FilePrecedence,
		Required:       false,
		Implied:        true,
		CatalogKey:     cand.entry.CatalogKey,
		Harnesses:      resolveEntryHarnesses(cand.entry, targetSet),
	}
}

func impliedCandidateExcluded(art ResolvedArtifact, cand requirementCandidate, cfg *config.Config, localExcludes resourceSelections, localExcludeAll map[ir.ResourceKind]bool) bool {
	if cand.entry.V4Decisions != nil {
		if !cand.entry.Required && v4OwnerExcluded(cand.entry, art.Kind, art.Name) {
			return true
		}
	} else if !cand.entry.Required {
		selector := cand.entry.SelectorForKind(art.Kind)
		if selector.ExcludeAll || slices.ContainsFunc(selector.Exclude, func(excluded string) bool {
			return strings.TrimSpace(excluded) == art.Name
		}) {
			return true
		}
	}
	probe := art
	probe.Required = cand.entry.Required
	if artifactShadowedByEntries(probe, cfg.Registries) {
		return true
	}
	return localResourceExcluded(localExcludes, localExcludeAll, art.Kind, art.Name, cand.entry.Required, art.Source)
}

// marketplaceHeldCandidate is a Marketplace pin held only through requires
// edges. It was resolved by the Marketplace resolver but diverted from the
// ordinary occurrence map. A requires declaration may admit it through the
// existing implied/preview path.
type marketplaceHeldCandidate struct {
	artifact ResolvedArtifact
	entry    config.MarketplaceEntry
	pin      config.MarketplaceResourcePin
}

// marketplaceReqOutcome reports how one Marketplace prerequisite declaration
// resolved: an admitted requires-held artifact, a preview-only lookup
// candidate, an unmet reason, or a lookup error.
type marketplaceReqOutcome struct {
	admitted *ResolvedArtifact
	preview  *ResolvedArtifact
	reason   UnmetRequirementReason
	err      error
}

func expandRequirements(
	resolved []ResolvedArtifact,
	cfg *config.Config,
	catalogs map[string][]catalog.Entry,
	resources map[string]map[string]ir.Resource,
	malformed *malformedIndex,
	localExcludes resourceSelections,
	localExcludeAll map[ir.ResourceKind]bool,
	targetSet []hcc.Target,
	ctx context.Context,
	held map[resourceSelection][]marketplaceHeldCandidate,
	lookup MarketplaceRequirementLookup,
	expansionEnabled bool,
) (implied []ResolvedArtifact, preview []ResolvedArtifact, unmet map[unmetKey]UnmetRequirementReason, errs []error) {
	hasRequirements := false
	for i := range resolved {
		if len(catalog.RequirementsFromResource(resolved[i].Resource)) > 0 {
			hasRequirements = true
			break
		}
	}
	if !hasRequirements {
		return nil, nil, nil, nil
	}
	unmet = make(map[unmetKey]UnmetRequirementReason)
	indexes := make(catalogIndexes)
	selected := make(map[resourceSelection]int, len(resolved))
	queue := make([]ResolvedArtifact, len(resolved))
	for i, artifact := range resolved {
		sel := resourceSelection{kind: artifact.Kind, name: artifact.Name}
		if _, dup := selected[sel]; !dup {
			selected[sel] = i
		}
		queue[i] = artifact
	}
	admitted := make(map[scopedSelection]int)

	for i := 0; i < len(queue); i++ {
		requirer := queue[i]
		for _, req := range catalog.RequirementsFromResource(requirer.Resource) {
			reqSel := resourceSelection{kind: req.Kind, name: strings.TrimSpace(req.Name)}
			if reqSel.name == "" {
				continue
			}
			key := newUnmetKey(requirer, req)
			ref := ResourceRef{Kind: requirer.Kind, Name: requirer.Name}
			scopeKey := scopedSelection{resourceSelection: reqSel, machine: requirer.Source.IsMachineScope()}
			if at, already := admitted[scopeKey]; already {
				if !slices.Contains(implied[at].RequiredBy, ref) {
					implied[at].RequiredBy = append(implied[at].RequiredBy, ref)
				}
				continue
			}
			if at, satisfied := selected[reqSel]; satisfied {
				if !slices.Contains(resolved[at].RequiredBy, ref) {
					resolved[at].RequiredBy = append(resolved[at].RequiredBy, ref)
				}
				continue
			}
			if _, seen := unmet[key]; seen {
				continue
			}
			if requirer.IsMarketplace {
				outcome := resolveMarketplaceRequirement(ctx, requirer, req, reqSel, ref, cfg, held[reqSel], lookup)
				switch {
				case outcome.err != nil:
					errs = append(errs, outcome.err)
					unmet[key] = UnmetReasonNotFound
				case outcome.admitted != nil:
					admitted[scopeKey] = len(implied)
					implied = append(implied, *outcome.admitted)
					queue = append(queue, *outcome.admitted)
				case outcome.preview != nil:
					preview = append(preview, *outcome.preview)
					if expansionEnabled {
						unmet[key] = UnmetReasonUnrecorded
					}
				default:
					unmet[key] = outcome.reason
				}
				continue
			}
			candidates, sourceMissing := requirementCandidates(requirer, req, cfg, catalogs, indexes)
			if sourceMissing {
				unmet[key] = UnmetReasonSourceNotConfigured
				continue
			}
			if len(candidates) == 0 {
				if hintEntries, missing := requirementCandidateEntries(requirer, req, cfg); !missing {
					for _, entry := range hintEntries {
						if sme := malformedSelectionError(malformed, entry.CatalogKey, req.Kind, reqSel.name, entry.URL, entry.Path, entry.Ref, false, "", &ref); sme != nil {
							errs = append(errs, sme)
							break
						}
					}
				}
				unmet[key] = UnmetReasonNotFound
				continue
			}

			sawExcluded := false
			admittedCandidate := false
			for _, cand := range candidates {
				art := impliedArtifact(cand, requirer, resources, targetSet)
				if impliedCandidateExcluded(art, cand, cfg, localExcludes, localExcludeAll) {
					sawExcluded = true
					continue
				}
				if len(targetSet) > 0 && len(art.Harnesses) == 0 {
					continue
				}
				art.RequiredBy = []ResourceRef{ref}
				admitted[scopeKey] = len(implied)
				implied = append(implied, art)
				queue = append(queue, art)
				admittedCandidate = true
				break
			}
			if admittedCandidate {
				continue
			}
			if sawExcluded {
				unmet[key] = UnmetReasonExcluded
			} else {
				unmet[key] = UnmetReasonNotFound
			}
		}
	}
	slog.Debug("requirement expansion complete", "implied", len(implied), "preview_only", len(preview), "unmet", len(unmet), "malformed_diagnostics", len(errs))
	return implied, preview, unmet, errs
}

// resolveMarketplaceRequirement resolves one prerequisite declared by a
// selected Marketplace resource. A requires-held pin whose edge names this
// requirer wins first (it is already resolved and recorded); otherwise the
// requirement targets the registry hint, or the requiring pin's own
// registry when there is none. Unbound hints are source-not-configured
// without a request. Found lookup candidates are preview-only and never
// recorded.
func resolveMarketplaceRequirement(ctx context.Context, requirer ResolvedArtifact, req catalog.Requirement, reqSel resourceSelection, ref ResourceRef, cfg *config.Config, held []marketplaceHeldCandidate, lookup MarketplaceRequirementLookup) marketplaceReqOutcome {
	requirerKind := mpapi.FromIR(requirer.Kind)
	for i := range held {
		cand := &held[i]
		if cand.entry.InstanceID != requirer.MarketplaceInstanceID {
			continue
		}
		matched := false
		for _, edge := range cand.pin.RequiresEdges.List() {
			if edge.RegistryID == requirer.MarketplaceRegistryID &&
				edge.Kind == requirerKind &&
				edge.Name == requirer.MarketplaceResourceName &&
				edge.Revision == requirer.MarketplaceDigest {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if cfg.MarketplacePinExcluded(cand.entry, cand.pin) {
			return marketplaceReqOutcome{reason: UnmetReasonExcluded}
		}
		art := cand.artifact
		art.Implied = true
		art.RequiredBy = []ResourceRef{ref}
		return marketplaceReqOutcome{admitted: &art}
	}

	entry := marketplaceEntryFor(requirer, cfg)
	if entry == nil {
		return marketplaceReqOutcome{reason: UnmetReasonNotFound}
	}
	bound := make(map[string]string, len(entry.Resources))
	var requirerPin *config.MarketplaceResourcePin
	for i := range entry.Resources {
		pin := &entry.Resources[i]
		if pin.RegistryID != "" {
			if _, ok := bound[pin.RegistryID]; !ok {
				bound[pin.RegistryID] = pin.RegistrySlug
			}
		}
		if pin.RegistryID == requirer.MarketplaceRegistryID &&
			pin.Kind == requirerKind &&
			pin.Name == requirer.MarketplaceResourceName &&
			pin.Digest == requirer.MarketplaceDigest {
			requirerPin = pin
		}
	}
	hint := strings.TrimSpace(req.Registry)
	var targetID, targetSlug string
	if hint == "" {
		targetID = requirer.MarketplaceRegistryID
		targetSlug = bound[targetID]
		if requirerPin != nil && requirerPin.RegistrySlug != "" {
			targetSlug = requirerPin.RegistrySlug
		}
	} else {
		found := false
		for _, pin := range entry.Resources {
			if pin.RegistrySlug == hint || pin.RegistryID == hint {
				targetID, targetSlug, found = pin.RegistryID, pin.RegistrySlug, true
				break
			}
		}
		if !found {
			return marketplaceReqOutcome{reason: UnmetReasonSourceNotConfigured}
		}
	}
	if lookup == nil {
		// Offline resolution or a Marketplace resolver without the lookup
		// extension: the requirement is unmet without network access.
		return marketplaceReqOutcome{reason: UnmetReasonNotFound}
	}
	query := MarketplaceRequirementQuery{
		Entry:        *entry,
		RegistryID:   targetID,
		RegistrySlug: targetSlug,
		SameRegistry: targetID == requirer.MarketplaceRegistryID,
		Kind:         mpapi.FromIR(reqSel.kind),
		Name:         reqSel.name,
		Path:         strings.TrimSpace(req.Path),
	}
	if query.SameRegistry && requirerPin != nil {
		query.RegistryCommit = requirerPin.RegistryCommit
	}
	result, err := lookup.LookupMarketplaceRequirement(ctx, query)
	if err != nil {
		return marketplaceReqOutcome{err: err}
	}
	if !result.Found {
		return marketplaceReqOutcome{reason: UnmetReasonNotFound}
	}
	candidatePin := config.MarketplaceResourcePin{
		RegistryID:   targetID,
		RegistrySlug: targetSlug,
		Kind:         query.Kind,
		Name:         query.Name,
	}
	if cfg.MarketplacePinExcluded(*entry, candidatePin) {
		return marketplaceReqOutcome{reason: UnmetReasonExcluded}
	}
	art := ResolvedArtifact{
		Entry:                   catalog.Entry{Kind: reqSel.kind, Name: reqSel.name},
		RequiredBy:              []ResourceRef{ref},
		Implied:                 true,
		PreviewOnly:             true,
		IsMarketplace:           true,
		MarketplaceURL:          entry.URL,
		MarketplaceInstanceID:   entry.InstanceID,
		MarketplaceAlias:        entry.Alias,
		MarketplaceRegistryID:   targetID,
		MarketplaceResourceName: query.Name,
		MarketplaceDigest:       result.ContentDigest,
		Source:                  entry.Source,
	}
	return marketplaceReqOutcome{preview: &art}
}

// marketplaceEntryFor returns the configured Marketplace entry that produced
// the requirer artifact, or nil when it cannot be found.
func marketplaceEntryFor(requirer ResolvedArtifact, cfg *config.Config) *config.MarketplaceEntry {
	for i := range cfg.Marketplaces {
		entry := &cfg.Marketplaces[i]
		if entry.InstanceID == requirer.MarketplaceInstanceID {
			return entry
		}
	}
	return nil
}
