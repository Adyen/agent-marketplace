package lockfile

import (
	"fmt"
	"sort"
)

type Operation uint8

const (
	OperationSync Operation = iota
	OperationUpgrade
)

type Requirements struct {
	Scope        Scope
	Git          []GitRequirement
	Marketplaces []MarketplaceRequirements
	Upgrade      UpgradeSelection
}

type GitRequirement struct {
	URL, Ref string
	Roots    []GitRootRequirement
}
type GitRootRequirement struct{ Path, SourceHarness string }
type MarketplaceRequirements struct {
	URL, InstanceID string
	Resources       []MarketplaceRequirement
}
type MarketplaceRequirement struct {
	SourceID, Kind, Name string
	Revision             Digest
	Collection           bool
}

type UpgradeSelection struct {
	All         bool
	Git         []GitUpgradeSelector
	Marketplace []MarketplaceUpgradeSelector
}
type GitUpgradeSelector struct{ URL, Ref, Path string }
type MarketplaceUpgradeSelector struct{ URL, InstanceID, SourceID, Kind, Name string }

func (s UpgradeSelection) SelectsGit(r GitRequirement) bool {
	if s.All || (len(s.Git) == 0 && len(s.Marketplace) == 0) {
		return true
	}
	for _, target := range s.Git {
		url, err := CanonicalGitURL(target.URL)
		if err != nil {
			continue
		}
		if url != r.URL {
			continue
		}
		if target.Ref != "" && target.Ref != r.Ref {
			continue
		}
		if target.Path == "" {
			return true
		}
		for _, root := range r.Roots {
			if root.Path == target.Path {
				return true
			}
		}
	}
	return false
}
func (s UpgradeSelection) SelectsMarketplace(endpoint MarketplaceRequirements, r MarketplaceRequirement) bool {
	if s.All || (len(s.Git) == 0 && len(s.Marketplace) == 0) {
		return true
	}
	for _, target := range s.Marketplace {
		if target.URL != "" {
			url, err := canonicalMarketplaceURL(target.URL)
			if err != nil || url != endpoint.URL {
				continue
			}
		}
		if target.InstanceID != "" && target.InstanceID != endpoint.InstanceID {
			continue
		}
		if target.SourceID != "" && target.SourceID != r.SourceID {
			continue
		}
		if target.Kind != "" && target.Kind != r.Kind {
			continue
		}
		if target.Name != "" && target.Name != r.Name {
			continue
		}
		return true
	}
	return false
}

type RecordKind string

const (
	RecordGit         RecordKind = "git"
	RecordMarketplace RecordKind = "marketplace"
)

type RecordRef struct {
	Kind     RecordKind
	Identity string
}
type AcquisitionRequest struct {
	Record      RecordRef
	Git         *GitRequirement
	Marketplace *MarketplaceAcquisition
}
type MarketplaceAcquisition struct {
	Endpoint    MarketplaceRequirements
	Requirement MarketplaceRequirement
}

type ResolutionPlan struct {
	Desired      Document
	Retained     []RecordRef
	Added        []RecordRef
	Invalidated  []RecordRef
	Upgraded     []RecordRef
	Removed      []RecordRef
	Acquisitions []AcquisitionRequest
}

func (p ResolutionPlan) Changed() bool {
	return len(p.Added)+len(p.Invalidated)+len(p.Upgraded)+len(p.Removed) > 0
}

func Plan(requirements Requirements, current *Document, operation Operation) (ResolutionPlan, error) {
	if !requirements.Scope.Valid() {
		return ResolutionPlan{}, coded(CodeInvalidScope, "invalid requirement scope %q", requirements.Scope)
	}
	if operation != OperationSync && operation != OperationUpgrade {
		return ResolutionPlan{}, coded(CodeInvalidDocument, "invalid lock operation")
	}
	plan := ResolutionPlan{Desired: Document{APIVersion: APIVersionV1, Scope: requirements.Scope}}
	var old Document
	if current != nil {
		normalized, err := Normalize(*current)
		if err != nil {
			return ResolutionPlan{}, err
		}
		if err = ValidateScope(normalized, requirements.Scope); err != nil {
			return ResolutionPlan{}, err
		}
		old = normalized
	} else {
		old = Document{APIVersion: APIVersionV1, Scope: requirements.Scope}
	}

	gitRequirements, err := coalesceGitRequirements(requirements.Git)
	if err != nil {
		return ResolutionPlan{}, err
	}
	oldGit := make(map[string]GitRegistry, len(old.Registries))
	for _, r := range old.Registries {
		oldGit[gitIdentity(r.URL, r.Ref)] = r
	}
	wantedGit := map[string]bool{}
	for _, req := range gitRequirements {
		id := gitIdentity(req.URL, req.Ref)
		if wantedGit[id] {
			continue
		}
		wantedGit[id] = true
		ref := RecordRef{Kind: RecordGit, Identity: id}
		locked, exists := oldGit[id]
		compatible := exists && rootsCompatible(req.Roots, locked.Roots)
		upgrade := operation == OperationUpgrade && requirements.Upgrade.SelectsGit(req)
		switch {
		case upgrade && compatible:
			plan.Upgraded = append(plan.Upgraded, ref)
			plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: ref, Git: &req})
		case compatible:
			plan.Retained = append(plan.Retained, ref)
			for _, root := range locked.Roots {
				if !requiresRoot(req.Roots, root) {
					plan.Removed = append(plan.Removed, RecordRef{Kind: RecordGit, Identity: id + "\x00root\x00" + root.Path + "\x00" + root.SourceHarness})
				}
			}
			locked.Roots = projectRoots(req.Roots, locked.Roots)
			plan.Desired.Registries = append(plan.Desired.Registries, locked)
		case exists:
			plan.Invalidated = append(plan.Invalidated, ref)
			plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: ref, Git: &req})
		default:
			plan.Added = append(plan.Added, ref)
			plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: ref, Git: &req})
		}
	}
	for _, r := range old.Registries {
		id := gitIdentity(r.URL, r.Ref)
		if !wantedGit[id] {
			plan.Removed = append(plan.Removed, RecordRef{Kind: RecordGit, Identity: id})
		}
	}

	marketplaceRequirements, err := coalesceMarketplaceRequirements(requirements.Marketplaces)
	if err != nil {
		return ResolutionPlan{}, err
	}
	oldMP := make(map[string]Marketplace, len(old.Marketplaces))
	for _, m := range old.Marketplaces {
		oldMP[marketplaceIdentity(m.URL, m.InstanceID)] = m
	}
	wantedMP := map[string]bool{}
	for _, endpoint := range marketplaceRequirements {
		eid := marketplaceIdentity(endpoint.URL, endpoint.InstanceID)
		wantedMP[eid] = true
		oldEndpoint, exists := oldMP[eid]
		desired := Marketplace{URL: endpoint.URL, InstanceID: endpoint.InstanceID}
		usedCollections := map[string]bool{}
		oldResources := map[string]MarketplaceResource{}
		oldCollections := map[string]MarketplaceCollection{}
		if exists {
			for _, r := range oldEndpoint.Resources {
				oldResources[marketplaceRequirementIdentity(r.SourceID, r.Kind, r.Name)] = r
			}
			for _, c := range oldEndpoint.Collections {
				oldCollections[marketplaceRequirementIdentity(c.SourceID, "collection", c.Name)] = c
			}
		}
		wantedRecords := map[string]bool{}
		for _, req := range endpoint.Resources {
			rid := marketplaceRequirementIdentity(req.SourceID, req.Kind, req.Name)
			if wantedRecords[rid] {
				continue
			}
			wantedRecords[rid] = true
			ref := RecordRef{Kind: RecordMarketplace, Identity: eid + "\x00" + rid}
			found := false
			recordExists := false
			var retainedResource MarketplaceResource
			var retainedCollection MarketplaceCollection
			if req.Collection {
				if c, ok := oldCollections[rid]; ok {
					recordExists = true
					found = req.Revision == "" || req.Revision == c.Revision
					retainedCollection = c
				}
			} else if r, ok := oldResources[rid]; ok {
				recordExists = true
				found = req.Revision == "" || req.Revision == r.Revision
				retainedResource = r
			}
			upgrade := operation == OperationUpgrade && req.Revision == "" && requirements.Upgrade.SelectsMarketplace(endpoint, req)
			switch {
			case upgrade && found:
				plan.Upgraded = append(plan.Upgraded, ref)
				acq := MarketplaceAcquisition{Endpoint: endpoint, Requirement: req}
				plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: ref, Marketplace: &acq})
			case found:
				plan.Retained = append(plan.Retained, ref)
				if req.Collection {
					desired.Collections = append(desired.Collections, retainedCollection)
					retainCollectionClosure(retainedCollection, oldCollections, oldEndpoint.Resources, usedCollections, &desired)
				} else {
					desired.Resources = appendResourceOnce(desired.Resources, retainedResource)
				}
			case recordExists:
				plan.Invalidated = append(plan.Invalidated, ref)
				acq := MarketplaceAcquisition{Endpoint: endpoint, Requirement: req}
				plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: ref, Marketplace: &acq})
			default:
				plan.Added = append(plan.Added, ref)
				acq := MarketplaceAcquisition{Endpoint: endpoint, Requirement: req}
				plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: ref, Marketplace: &acq})
			}
		}
		rebuildMarketplaceGraph(&desired, endpoint.Resources)
		if len(desired.Resources) > 0 || len(desired.Collections) > 0 {
			plan.Desired.Marketplaces = append(plan.Desired.Marketplaces, desired)
		}
		if exists {
			for _, r := range oldEndpoint.Resources {
				rid := marketplaceRequirementIdentity(r.SourceID, r.Kind, r.Name)
				if !wantedRecords[rid] && !containsMarketplaceResource(desired.Resources, r) {
					plan.Removed = append(plan.Removed, RecordRef{Kind: RecordMarketplace, Identity: eid + "\x00" + rid})
				}
			}
			for _, c := range oldEndpoint.Collections {
				rid := marketplaceRequirementIdentity(c.SourceID, "collection", c.Name)
				if !wantedRecords[rid] && !containsCollection(desired.Collections, c) {
					plan.Removed = append(plan.Removed, RecordRef{Kind: RecordMarketplace, Identity: eid + "\x00" + rid})
				}
			}
		}
	}
	for _, m := range old.Marketplaces {
		eid := marketplaceIdentity(m.URL, m.InstanceID)
		if !wantedMP[eid] {
			for _, r := range m.Resources {
				plan.Removed = append(plan.Removed, RecordRef{Kind: RecordMarketplace, Identity: eid + "\x00" + marketplaceRequirementIdentity(r.SourceID, r.Kind, r.Name)})
			}
			for _, c := range m.Collections {
				plan.Removed = append(plan.Removed, RecordRef{Kind: RecordMarketplace, Identity: eid + "\x00" + marketplaceRequirementIdentity(c.SourceID, "collection", c.Name)})
			}
		}
	}
	sortPlan(&plan)
	return plan, nil
}

func coalesceGitRequirements(input []GitRequirement) ([]GitRequirement, error) {
	byIdentity := make(map[string]GitRequirement, len(input))
	for _, raw := range input {
		requirement, err := normalizeGitRequirement(raw)
		if err != nil {
			return nil, err
		}
		identity := gitIdentity(requirement.URL, requirement.Ref)
		combined := byIdentity[identity]
		if combined.URL == "" {
			combined.URL, combined.Ref = requirement.URL, requirement.Ref
		}
		for _, root := range requirement.Roots {
			found := false
			for _, existing := range combined.Roots {
				if existing == root {
					found = true
					break
				}
			}
			if !found {
				combined.Roots = append(combined.Roots, root)
			}
		}
		byIdentity[identity] = combined
	}
	out := make([]GitRequirement, 0, len(byIdentity))
	for _, requirement := range byIdentity {
		sort.Slice(requirement.Roots, func(i, j int) bool {
			if requirement.Roots[i].Path != requirement.Roots[j].Path {
				return requirement.Roots[i].Path < requirement.Roots[j].Path
			}
			return requirement.Roots[i].SourceHarness < requirement.Roots[j].SourceHarness
		})
		out = append(out, requirement)
	}
	sort.Slice(out, func(i, j int) bool { return gitIdentity(out[i].URL, out[i].Ref) < gitIdentity(out[j].URL, out[j].Ref) })
	return out, nil
}

func coalesceMarketplaceRequirements(input []MarketplaceRequirements) ([]MarketplaceRequirements, error) {
	byIdentity := make(map[string]MarketplaceRequirements, len(input))
	for _, raw := range input {
		endpoint, err := normalizeMarketplaceRequirements(raw)
		if err != nil {
			return nil, err
		}
		identity := marketplaceIdentity(endpoint.URL, endpoint.InstanceID)
		combined := byIdentity[identity]
		if combined.URL == "" {
			combined.URL, combined.InstanceID = endpoint.URL, endpoint.InstanceID
		}
		for _, requirement := range endpoint.Resources {
			recordIdentity := marketplaceRequirementIdentity(requirement.SourceID, requirement.Kind, requirement.Name)
			found := false
			for i := range combined.Resources {
				existing := &combined.Resources[i]
				if marketplaceRequirementIdentity(existing.SourceID, existing.Kind, existing.Name) != recordIdentity {
					continue
				}
				found = true
				if existing.Revision != "" && requirement.Revision != "" && existing.Revision != requirement.Revision {
					return nil, coded(CodeIdentityConflict, "Marketplace requirement %q has conflicting exact revisions", recordIdentity)
				}
				if existing.Revision == "" {
					existing.Revision = requirement.Revision
				}
				break
			}
			if !found {
				combined.Resources = append(combined.Resources, requirement)
			}
		}
		byIdentity[identity] = combined
	}
	out := make([]MarketplaceRequirements, 0, len(byIdentity))
	for _, endpoint := range byIdentity {
		sort.Slice(endpoint.Resources, func(i, j int) bool {
			a, b := endpoint.Resources[i], endpoint.Resources[j]
			return marketplaceRequirementIdentity(a.SourceID, a.Kind, a.Name) < marketplaceRequirementIdentity(b.SourceID, b.Kind, b.Name)
		})
		out = append(out, endpoint)
	}
	sort.Slice(out, func(i, j int) bool {
		return marketplaceIdentity(out[i].URL, out[i].InstanceID) < marketplaceIdentity(out[j].URL, out[j].InstanceID)
	})
	return out, nil
}

func normalizeGitRequirement(r GitRequirement) (GitRequirement, error) {
	var err error
	r.URL, err = CanonicalGitURL(r.URL)
	if err != nil {
		return r, err
	}
	if r.Ref == "" {
		return r, coded(CodeInvalidDocument, "Git requirement ref is required")
	}
	for i := range r.Roots {
		r.Roots[i].Path, err = normalizeArtifactPath(r.Roots[i].Path, true)
		if err != nil {
			return r, err
		}
	}
	sort.Slice(r.Roots, func(i, j int) bool {
		if r.Roots[i].Path != r.Roots[j].Path {
			return r.Roots[i].Path < r.Roots[j].Path
		}
		return r.Roots[i].SourceHarness < r.Roots[j].SourceHarness
	})
	return r, nil
}
func normalizeMarketplaceRequirements(m MarketplaceRequirements) (MarketplaceRequirements, error) {
	var err error
	m.URL, err = canonicalMarketplaceURL(m.URL)
	if err != nil {
		return m, err
	}
	if m.InstanceID == "" {
		return m, coded(CodeInvalidDocument, "Marketplace instance ID is required")
	}
	for i := range m.Resources {
		r := &m.Resources[i]
		if r.Collection {
			r.Kind = "collection"
		}
		if err = requiredCoordinate(r.SourceID, r.Kind, r.Name); err != nil {
			return m, err
		}
		if r.Revision != "" {
			if err = validateDigest(r.Revision, "exact revision"); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}
func gitIdentity(url, ref string) string        { return url + "\x00" + ref }
func marketplaceIdentity(url, id string) string { return id + "\x00" + url }
func marketplaceRequirementIdentity(source, kind, name string) string {
	return source + "\x00" + kind + "\x00" + name
}
func gitRootMatches(want GitRootRequirement, got GitRoot) bool {
	return want.Path == got.Path && (want.SourceHarness == "" || want.SourceHarness == got.SourceHarness)
}

func rootsCompatible(req []GitRootRequirement, locked []GitRoot) bool {
	for _, want := range req {
		found := false
		for _, got := range locked {
			if gitRootMatches(want, got) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func requiresRoot(req []GitRootRequirement, locked GitRoot) bool {
	for _, want := range req {
		if gitRootMatches(want, locked) {
			return true
		}
	}
	return false
}

func projectRoots(req []GitRootRequirement, locked []GitRoot) []GitRoot {
	out := make([]GitRoot, 0, len(req))
	seen := make(map[string]bool, len(req))
	for _, want := range req {
		for _, got := range locked {
			if !gitRootMatches(want, got) {
				continue
			}
			key := got.Path + "\x00" + got.SourceHarness
			if !seen[key] {
				out = append(out, got)
				seen[key] = true
			}
			if want.SourceHarness != "" {
				break
			}
		}
	}
	return out
}
func appendResourceOnce(v []MarketplaceResource, r MarketplaceResource) []MarketplaceResource {
	if containsMarketplaceResource(v, r) {
		return v
	}
	return append(v, r)
}
func containsMarketplaceResource(v []MarketplaceResource, r MarketplaceResource) bool {
	for _, x := range v {
		if resourceKey(x) == resourceKey(r) {
			return true
		}
	}
	return false
}
func containsCollection(v []MarketplaceCollection, c MarketplaceCollection) bool {
	for _, x := range v {
		if collectionKey(x) == collectionKey(c) {
			return true
		}
	}
	return false
}
func retainCollectionClosure(c MarketplaceCollection, all map[string]MarketplaceCollection, resources []MarketplaceResource, seen map[string]bool, out *Marketplace) {
	key := collectionKey(c)
	if seen[key] {
		return
	}
	seen[key] = true
	if !containsCollection(out.Collections, c) {
		out.Collections = append(out.Collections, c)
	}
	for _, edge := range c.Members {
		if edge.Kind == "collection" {
			if child, ok := all[marketplaceRequirementIdentity(edge.SourceID, "collection", edge.Name)]; ok && child.Revision == edge.Revision {
				retainCollectionClosure(child, all, resources, seen, out)
			}
		} else {
			for _, r := range resources {
				if r.SourceID == edge.SourceID && r.Kind == edge.Kind && r.Name == edge.Name && r.Revision == edge.Revision {
					out.Resources = appendResourceOnce(out.Resources, r)
				}
			}
		}
	}
}

func ReconcileMarketplaceGraph(doc *Document, requirements []MarketplaceRequirements) error {
	coalesced, err := coalesceMarketplaceRequirements(requirements)
	if err != nil {
		return err
	}
	byEndpoint := make(map[string]MarketplaceRequirements, len(coalesced))
	for _, endpoint := range coalesced {
		byEndpoint[marketplaceIdentity(endpoint.URL, endpoint.InstanceID)] = endpoint
	}
	out := doc.Marketplaces[:0]
	for i := range doc.Marketplaces {
		market := &doc.Marketplaces[i]
		endpoint, ok := byEndpoint[marketplaceIdentity(market.URL, market.InstanceID)]
		if !ok {
			continue
		}
		rebuildMarketplaceGraph(market, endpoint.Resources)
		if len(market.Resources) > 0 || len(market.Collections) > 0 {
			out = append(out, *market)
		}
	}
	doc.Marketplaces = out
	return nil
}

func rebuildMarketplaceGraph(market *Marketplace, requirements []MarketplaceRequirement) {
	directResources := map[string]bool{}
	directCollections := map[string]bool{}
	for _, requirement := range requirements {
		if requirement.Collection || requirement.Kind == "collection" {
			directCollections[marketplaceRequirementIdentity(requirement.SourceID, "collection", requirement.Name)] = true
		} else {
			directResources[marketplaceRequirementIdentity(requirement.SourceID, requirement.Kind, requirement.Name)] = true
		}
	}

	collections := make(map[string]MarketplaceCollection, len(market.Collections))
	for i := range market.Collections {
		collection := &market.Collections[i]
		collection.Direct = directCollections[marketplaceRequirementIdentity(collection.SourceID, "collection", collection.Name)]
		collections[collectionKey(*collection)] = *collection
	}
	resources := make(map[string]MarketplaceResource, len(market.Resources))
	for i := range market.Resources {
		resource := &market.Resources[i]
		resource.Direct = directResources[marketplaceRequirementIdentity(resource.SourceID, resource.Kind, resource.Name)]
		resource.SelectedBy = nil
		resources[resourceKey(*resource)] = *resource
	}

	reachableCollections := map[string]bool{}
	selectors := map[string][]CollectionEdge{}
	var visit func(MarketplaceCollection, CollectionEdge, map[string]bool)
	visit = func(collection MarketplaceCollection, root CollectionEdge, visiting map[string]bool) {
		key := collectionKey(collection)
		if visiting[key] {
			return
		}
		reachableCollections[key] = true
		visiting[key] = true
		defer delete(visiting, key)
		for _, edge := range collection.Members {
			if edge.Kind == "collection" {
				if child, ok := collections[edge.SourceID+"\x00"+edge.Name+"\x00"+edge.Revision.String()]; ok {
					visit(child, root, visiting)
				}
				continue
			}
			resourceKey := edge.SourceID + "\x00" + edge.Kind + "\x00" + edge.Name + "\x00" + edge.Revision.String()
			if _, ok := resources[resourceKey]; ok {
				selectors[resourceKey] = append(selectors[resourceKey], root)
			}
		}
	}
	for _, collection := range market.Collections {
		if collection.Direct {
			root := CollectionEdge{SourceID: collection.SourceID, Name: collection.Name, Revision: collection.Revision}
			visit(collection, root, map[string]bool{})
		}
	}

	keptCollections := market.Collections[:0]
	for _, collection := range market.Collections {
		if !reachableCollections[collectionKey(collection)] {
			continue
		}
		keptCollections = append(keptCollections, collection)
	}
	market.Collections = keptCollections
	keptResources := market.Resources[:0]
	seenResources := map[string]bool{}
	for _, resource := range market.Resources {
		key := resourceKey(resource)
		if seenResources[key] {
			continue
		}
		resource.SelectedBy = selectors[key]
		if !resource.Direct && len(resource.SelectedBy) == 0 {
			continue
		}
		seenResources[key] = true
		keptResources = append(keptResources, resource)
	}
	market.Resources = keptResources
}

func sortPlan(p *ResolutionPlan) {
	less := func(v []RecordRef) {
		sort.Slice(v, func(i, j int) bool {
			if v[i].Kind != v[j].Kind {
				return v[i].Kind < v[j].Kind
			}
			return v[i].Identity < v[j].Identity
		})
	}
	less(p.Retained)
	less(p.Added)
	less(p.Invalidated)
	less(p.Upgraded)
	less(p.Removed)
	sort.Slice(p.Acquisitions, func(i, j int) bool {
		return fmt.Sprint(p.Acquisitions[i].Record) < fmt.Sprint(p.Acquisitions[j].Record)
	})
	sort.Slice(p.Desired.Registries, func(i, j int) bool {
		return gitIdentity(p.Desired.Registries[i].URL, p.Desired.Registries[i].Ref) < gitIdentity(p.Desired.Registries[j].URL, p.Desired.Registries[j].Ref)
	})
	sort.Slice(p.Desired.Marketplaces, func(i, j int) bool {
		return marketplaceIdentity(p.Desired.Marketplaces[i].URL, p.Desired.Marketplaces[i].InstanceID) < marketplaceIdentity(p.Desired.Marketplaces[j].URL, p.Desired.Marketplaces[j].InstanceID)
	})
}
