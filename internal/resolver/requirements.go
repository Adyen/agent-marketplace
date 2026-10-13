package resolver

import (
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

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
)

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

func expandRequirements(
	resolved []ResolvedArtifact,
	cfg *config.Config,
	catalogs map[string][]catalog.Entry,
	resources map[string]map[string]ir.Resource,
	malformed *malformedIndex,
	localExcludes resourceSelections,
	localExcludeAll map[ir.ResourceKind]bool,
	targetSet []hcc.Target,
) (implied []ResolvedArtifact, unmet map[unmetKey]UnmetRequirementReason, errs []error) {
	hasRequirements := false
	for i := range resolved {
		if len(catalog.RequirementsFromResource(resolved[i].Resource)) > 0 {
			hasRequirements = true
			break
		}
	}
	if !hasRequirements {
		return nil, nil, nil
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
	slog.Debug("requirement expansion complete", "implied", len(implied), "unmet", len(unmet), "malformed_diagnostics", len(errs))
	return implied, unmet, errs
}
