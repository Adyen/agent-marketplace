package marketplace

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// PreparedInstall is the verified, immutable input to a Marketplace config
// mutation. The artifact has already passed Resolver's archive, content, and
// manifest checks before this value is returned.
type PreparedInstall struct {
	Entry         config.MarketplaceEntry
	Pin           config.MarketplaceResourcePin
	Revision      store.AssessedResourceRevision
	Resolved      resolver.ResolvedArtifact
	Artifact      hccartifact.CanonicalArtifact
	ArchiveDigest string

	// CollectionMembers contains the fully verified leaf plans for a virtual
	// collection. The collection itself is never passed to materialization.
	CollectionMembers  []*PreparedInstall
	CollectionManifest hccartifact.CollectionManifest
	PreparationWork    int
}

// InstallPreview is the stable, typed preview surface for a prepared install.
type InstallPreview struct {
	MarketplaceAlias       string
	MarketplaceURL         string
	InstanceID             string
	RegistryID             string
	RegistrySlug           string
	Kind                   string
	Name                   string
	InstalledName          string
	Revision               string
	ContentDigest          string
	ArchiveDigest          string
	Acquisition            string
	RegistryCommit         string
	RegistryManifestDigest string
	License                string
	Findings               []hccartifact.CanonicalFinding
	Capabilities           []string
	Provenance             hccartifact.CanonicalProvenance
	CollectionMembers      []InstallPreview
}

// Preview returns metadata decoded from the verified artifact, not from an
// untrusted index response. Virtual collections use their verified revision
// metadata for the root and recursively expose each verified member preview.
func (p *PreparedInstall) Preview() InstallPreview {
	installedName := p.Pin.InstalledName
	if installedName == "" {
		installedName = p.Pin.Name
	}
	preview := InstallPreview{
		MarketplaceAlias: p.Entry.Alias, MarketplaceURL: p.Entry.URL, InstanceID: p.Entry.InstanceID,
		RegistryID: p.Pin.RegistryID, RegistrySlug: p.Pin.RegistrySlug, Kind: p.Pin.Kind, Name: p.Pin.Name,
		InstalledName: installedName, Revision: p.Pin.Revision, ContentDigest: p.Pin.Digest,
		ArchiveDigest: p.ArchiveDigest, Acquisition: p.Pin.Acquisition,
		RegistryCommit: p.Pin.RegistryCommit, RegistryManifestDigest: p.Pin.RegistryManifestDigest,
		License: p.Artifact.Manifest.License, Findings: canonicalFindingsFromRevision(p.Revision),
		Capabilities: p.Artifact.Manifest.Capabilities, Provenance: p.Artifact.Manifest.Provenance,
	}
	if p.Pin.Kind == "collection" || len(p.CollectionMembers) > 0 {
		if preview.License == "" {
			preview.License = p.Revision.License
		}
		if preview.Provenance == (hccartifact.CanonicalProvenance{}) {
			preview.Provenance = p.CollectionManifest.Provenance
		}
		preview.CollectionMembers = make([]InstallPreview, len(p.CollectionMembers))
		for i := range p.CollectionMembers {
			preview.CollectionMembers[i] = p.CollectionMembers[i].Preview()
		}
	}
	return preview
}

func canonicalFindingsFromRevision(revision store.AssessedResourceRevision) []hccartifact.CanonicalFinding {
	if len(revision.Assessment.Findings) == 0 {
		return nil
	}
	findings := make([]hccartifact.CanonicalFinding, len(revision.Assessment.Findings))
	for i, finding := range revision.Assessment.Findings {
		findings[i] = hccartifact.CanonicalFinding{
			Code: finding.Code, Severity: finding.Severity, Message: finding.Message,
			PolicyOutcome: finding.Policy, AnalyzerVersion: revision.Assessment.AnalyzerVersion,
		}
		if finding.Location != nil {
			findings[i].File, findings[i].Line, findings[i].Column = finding.Location.Path, finding.Location.Line, finding.Location.Column
		}
	}
	return findings
}

// LatestRevision verifies the endpoint and registry identity, then fetches the
// latest metadata for an existing pin. Index-only pins also validate the exact
// registry occurrence, but this method does not fetch an artifact.
func (r *Resolver) LatestRevision(ctx context.Context, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) (store.Registry, store.AssessedResourceRevision, error) {
	if err := ctx.Err(); err != nil {
		return store.Registry{}, store.AssessedResourceRevision{}, err
	}
	canonical, err := canonicalMarketplaceEntry(entry)
	if err != nil {
		return store.Registry{}, store.AssessedResourceRevision{}, err
	}
	client, err := r.clientForEntry(canonical)
	if err != nil {
		return store.Registry{}, store.AssessedResourceRevision{}, err
	}
	wellKnown, err := client.WellKnown(ctx)
	if err != nil {
		return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("well-known document: %w", err)
	}
	if wellKnown.InstanceID != canonical.InstanceID {
		return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("instance ID mismatch: endpoint %q, configured %q", wellKnown.InstanceID, canonical.InstanceID)
	}
	registry, err := client.GetRegistryBySlug(ctx, marketplaceAPIRegistry(pin))
	if err != nil {
		return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("resolve registry %q: %w", pin.RegistryID, err)
	}
	if registry.ID != store.RegistryID(pin.RegistryID) {
		return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("registry identity mismatch: got %q, want %q", registry.ID, pin.RegistryID)
	}
	if registry.Acquisition != store.AcquisitionMirror && registry.Acquisition != store.AcquisitionIndexOnly {
		return registry, store.AssessedResourceRevision{}, fmt.Errorf("registry uses unsupported acquisition %q", registry.Acquisition)
	}
	resourceRegistry := registry.Slug
	if registry.Acquisition == store.AcquisitionIndexOnly {
		resourceRegistry = string(registry.ID)
	}
	if resourceRegistry == "" {
		resourceRegistry = string(registry.ID)
	}
	latest, err := client.GetLatestResourceRevision(ctx, resourceRegistry, pin.Kind, pin.Name)
	if err != nil {
		return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("resolve latest resource revision: %w", err)
	}
	if err := validateLatestResourceRevision(canonical, pin, latest); err != nil {
		return store.Registry{}, store.AssessedResourceRevision{}, err
	}
	if err := enforceMarketplaceUse(latest, nil); err != nil {
		return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("latest revision: %w", err)
	}
	if registry.Acquisition == store.AcquisitionIndexOnly {
		if latest.Occurrence.Coordinate.Registry != registry.ID || !mpapi.IsCanonicalGitCommit(latest.Occurrence.Commit) || latest.Occurrence.ManifestDigest == "" {
			return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("latest index-only revision has no exact registry occurrence")
		}
		occurrence, err := client.GetRegistryOccurrence(ctx, string(registry.ID), latest.Occurrence.Commit)
		if err != nil {
			return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("exact registry commit unavailable: %w", err)
		}
		if occurrence.Acquisition != store.AcquisitionIndexOnly || occurrence.RegistryRevisionManifestDigest != latest.Occurrence.ManifestDigest {
			return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("registry manifest digest mismatch for latest index-only revision")
		}
		origin := strings.TrimSpace(occurrence.AcquisitionOrigin)
		if err := validateAcquisitionOrigin(origin); err != nil {
			return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("policy rejection: invalid acquisition origin: %w", err)
		}
		if err := config.ValidateRegistryURLs([]config.RegistryEntry{{URL: origin, Ref: occurrence.Ref}}, r.AllowedRegistries); err != nil {
			return store.Registry{}, store.AssessedResourceRevision{}, fmt.Errorf("policy rejection: acquisition origin rejected by allowed_registries: %w", err)
		}
	}
	return registry, latest, nil
}

func validateLocatorRevision(locator MarketplaceLocator, revision store.AssessedResourceRevision) error {
	expectedDigest := strings.TrimSpace(locator.Digest)
	if expectedDigest != "" {
		if err := validateSHA256Digest(expectedDigest); err != nil {
			return fmt.Errorf("requested content digest %q: %w", locator.Digest, err)
		}
	}
	if revisionDigest := digestForRevision(locator.Revision); revisionDigest != "" {
		if expectedDigest != "" && expectedDigest != revisionDigest {
			return fmt.Errorf("requested revision and digest disagree: revision %q, digest %q", locator.Revision, locator.Digest)
		}
		expectedDigest = revisionDigest
	}
	if expectedDigest != "" && revision.ContentDigest != store.RevisionDigest(expectedDigest) {
		return fmt.Errorf("exact revision content digest mismatch: response %q, requested %q", revision.ContentDigest, expectedDigest)
	}
	return nil
}

// PrepareLocator resolves a user-facing locator and fully prefetches and
// verifies its mirror artifact. It deliberately does not edit configuration.
func (r *Resolver) PrepareLocator(ctx context.Context, entry config.MarketplaceEntry, locator MarketplaceLocator, targets []hcc.Target) (*PreparedInstall, error) {
	return r.prepareLocator(ctx, entry, locator, targets, 0, nil)
}

// PrepareLocatorWithResourceLimit is PrepareLocator with an upper bound on
// collection traversal work and an optional per-invocation revocation policy.
func (r *Resolver) PrepareLocatorWithResourceLimit(ctx context.Context, entry config.MarketplaceEntry, locator MarketplaceLocator, targets []hcc.Target, resourceLimit int, policies ...resolver.MarketplaceRevocationPolicy) (*PreparedInstall, error) {
	if resourceLimit < 1 {
		return nil, fmt.Errorf("marketplace prepared resource limit must be positive")
	}
	var policy resolver.MarketplaceRevocationPolicy
	if len(policies) > 0 {
		policy = policies[0]
	}
	return r.prepareLocator(ctx, entry, locator, targets, resourceLimit, policy)
}

type collectionPreparationBudget struct {
	limit int
	used  int
}

func (b *collectionPreparationBudget) consume(count int) error {
	if b == nil || count == 0 {
		return nil
	}
	if count < 0 || (b.limit > 0 && b.used > b.limit-count) {
		return fmt.Errorf("marketplace collection preparation work exceeds resource limit %d", b.limit)
	}
	b.used += count
	return nil
}

func (r *Resolver) prepareLocator(ctx context.Context, entry config.MarketplaceEntry, locator MarketplaceLocator, targets []hcc.Target, resourceLimit int, policy resolver.MarketplaceRevocationPolicy) (*PreparedInstall, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canonical, err := canonicalMarketplaceEntry(entry)
	if err != nil {
		return nil, err
	}
	client, err := r.clientForEntry(canonical)
	if err != nil {
		return nil, err
	}
	wellKnown, err := client.WellKnown(ctx)
	if err != nil {
		return nil, fmt.Errorf("well-known document: %w", err)
	}
	if strings.TrimSpace(wellKnown.InstanceID) == "" {
		return nil, fmt.Errorf("well-known document has no instance ID")
	}
	if strings.TrimSpace(canonical.InstanceID) == "" {
		canonical.InstanceID = wellKnown.InstanceID
	} else if wellKnown.InstanceID != canonical.InstanceID {
		return nil, fmt.Errorf("instance ID mismatch: endpoint %q, configured %q", wellKnown.InstanceID, canonical.InstanceID)
	}

	if locator.Kind == "" {
		return nil, fmt.Errorf("marketplace resource kind is required")
	}
	registry, err := client.GetRegistryBySlug(ctx, locator.Registry)
	if err != nil {
		return nil, fmt.Errorf("resolve registry %q: %w", locator.Registry, err)
	}
	if registry.Acquisition != store.AcquisitionMirror && registry.Acquisition != store.AcquisitionIndexOnly {
		return nil, fmt.Errorf("registry %q uses unsupported acquisition %q", locator.Registry, registry.Acquisition)
	}

	var revision store.AssessedResourceRevision
	exactRevision := locator.Revision
	if exactRevision == "" {
		exactRevision = locator.Digest
	}
	// Index-only occurrence lookup is keyed by immutable Registry ID, while
	// preserving the existing slug route for mirror revision lookups.
	resourceRegistry := locator.Registry
	if registry.Acquisition == store.AcquisitionIndexOnly || resourceRegistry == "" {
		resourceRegistry = string(registry.ID)
	}
	if exactRevision != "" {
		revision, err = client.GetResourceRevisionForRegistry(ctx, resourceRegistry, string(registry.ID), locator.Kind, locator.Name, exactRevision)
	} else {
		revision, err = client.GetLatestResourceRevisionForRegistry(ctx, resourceRegistry, string(registry.ID), locator.Kind, locator.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve resource revision: %w", err)
	}
	if exactRevision != "" {
		if err := validateLocatorRevision(locator, revision); err != nil {
			return nil, err
		}
	}
	if err := enforceMarketplaceUse(revision, policy); err != nil {
		return nil, fmt.Errorf("resource revision: %w", err)
	}
	pinRevision := locator.Revision
	if pinRevision == "" {
		pinRevision = string(revision.ContentDigest)
	}
	pin := config.MarketplaceResourcePin{
		RegistryID: string(registry.ID), RegistrySlug: registry.Slug, Kind: locator.Kind,
		Name: locator.Name, Revision: pinRevision, Digest: string(revision.ContentDigest),
		Acquisition: config.MarketplaceAcquisitionMirror,
	}
	if registry.Acquisition == store.AcquisitionIndexOnly {
		if revision.Occurrence.Commit == "" {
			return nil, fmt.Errorf("index-only resource revision has no exact registry occurrence")
		}
		occurrence, err := client.GetRegistryOccurrence(ctx, string(registry.ID), revision.Occurrence.Commit)
		if err != nil {
			return nil, fmt.Errorf("resolve exact registry occurrence: %w", err)
		}
		if occurrence.Acquisition != store.AcquisitionIndexOnly {
			return nil, fmt.Errorf("resource revision occurrence is not index-only")
		}
		if revision.Occurrence.Coordinate.Registry != registry.ID || revision.Occurrence.Coordinate.Registry != occurrence.RegistryID {
			return nil, fmt.Errorf("resource revision provenance registry ID does not match exact registry occurrence")
		}
		if occurrence.RegistryRevisionManifestDigest != revision.Occurrence.ManifestDigest {
			return nil, fmt.Errorf("resource revision and registry occurrence manifest digests differ")
		}
		pin.Acquisition = config.MarketplaceAcquisitionIndexOnly
		pin.RegistryCommit = occurrence.Commit
		pin.RegistryManifestDigest = string(occurrence.RegistryRevisionManifestDigest)
	}

	cache := r.cache()
	if locator.Kind == "collection" {
		budget := &collectionPreparationBudget{limit: resourceLimit}
		if err := budget.consume(1); err != nil {
			return nil, err
		}
		if registry.Acquisition != store.AcquisitionMirror {
			return nil, fmt.Errorf("collection %q requires mirror acquisition", locator.Name)
		}
		if err := enforceMarketplaceUse(revision, policy); err != nil {
			return nil, fmt.Errorf("collection revision: %w", err)
		}
		if _, err := cache.WriteResourceRevision(canonical.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, revision); err != nil {
			return nil, fmt.Errorf("cache collection revision: %w", err)
		}
		manifest, manifestPayload, err := client.GetCollectionManifest(ctx, string(revision.ContentDigest))
		if err != nil {
			return nil, fmt.Errorf("fetch collection manifest: %w", err)
		}
		if _, err := cache.WriteCollectionManifest(string(revision.ContentDigest), manifestPayload); err != nil {
			return nil, fmt.Errorf("cache collection manifest: %w", err)
		}
		if manifest.Name != locator.Name {
			return nil, fmt.Errorf("collection manifest name mismatch: got %q, want %q", manifest.Name, locator.Name)
		}
		if err := cache.MarkCollectionVerified(string(revision.ContentDigest)); err != nil {
			return nil, fmt.Errorf("mark collection manifest verified: %w", err)
		}
		nestedManifests := make(map[string]hccartifact.CollectionManifest)
		if err := r.verifyNestedCollectionManifests(ctx, client, cache, canonical, manifest, map[string]bool{string(revision.ContentDigest): true}, nestedManifests, budget, policy); err != nil {
			return nil, err
		}
		leaves, err := hccartifact.ValidateAndExpandCollectionManifestGraph(manifest, nestedManifests)
		if err != nil {
			return nil, fmt.Errorf("validate collection graph: %w", err)
		}
		uniqueLeaves := make([]hccartifact.CollectionMemberEntry, 0, len(leaves))
		seen := make(map[string]string, len(leaves))
		for _, member := range leaves {
			key := member.RegistryID + "/" + member.Kind + "/" + member.Name
			if previous, ok := seen[key]; ok {
				if previous != member.Revision {
					return nil, fmt.Errorf("collection contains conflicting revisions for %s", key)
				}
				continue
			}
			seen[key] = member.Revision
			uniqueLeaves = append(uniqueLeaves, member)
		}
		if err := budget.consume(len(uniqueLeaves)); err != nil {
			return nil, err
		}
		members := make([]*PreparedInstall, 0, len(uniqueLeaves))
		for _, member := range uniqueLeaves {
			memberPlan, err := r.prepareCollectionLeaf(ctx, client, cache, canonical, registry, revision, member, targets, policy)
			if err != nil {
				return nil, err
			}
			members = append(members, memberPlan)
		}
		slog.Info("marketplace collection install plan prepared", "url", canonical.URL, "instance_id", canonical.InstanceID, "registry_id", registry.ID, "name", locator.Name, "manifest_digest", revision.ContentDigest, "members", len(members))
		return &PreparedInstall{Entry: canonical, Pin: pin, Revision: revision, CollectionManifest: manifest, CollectionMembers: members, PreparationWork: budget.used}, nil
	}
	resolved, artifact, err := r.resolvePinWithArtifactPolicy(ctx, client, cache, canonical, pin, targets, resolver.MarketplaceResolveModeRefresh, policy, r.AllowedRegistries)
	if err != nil {
		return nil, fmt.Errorf("verify Marketplace artifact: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	archiveDigest := string(revision.ArchiveDigest)
	if archiveDigest == "" {
		archiveDigest = artifact.Manifest.ArchiveDigest
	}
	slog.Info("marketplace install plan prepared", "url", canonical.URL, "instance_id", canonical.InstanceID, "registry_id", registry.ID, "kind", locator.Kind, "name", locator.Name, "digest", pin.Digest, "acquisition", pin.Acquisition, "registry_commit", pin.RegistryCommit)
	return &PreparedInstall{Entry: canonical, Pin: pin, Revision: revision, Resolved: resolved, Artifact: artifact, ArchiveDigest: archiveDigest, PreparationWork: 1}, nil
}

func (r *Resolver) verifyNestedCollectionManifests(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, manifest hccartifact.CollectionManifest, visiting map[string]bool, nestedManifests map[string]hccartifact.CollectionManifest, budget *collectionPreparationBudget, policy resolver.MarketplaceRevocationPolicy) error {
	for _, member := range manifest.Members {
		if member.Kind != "collection" {
			continue
		}
		if err := budget.consume(1); err != nil {
			return err
		}
		if member.RegistryID == "" || member.Revision == "" {
			return fmt.Errorf("collection %q has an unresolved nested member %q", manifest.Name, member.Name)
		}
		if visiting[member.Revision] {
			return fmt.Errorf("collection cycle detected at %q", member.Name)
		}
		nestedRegistry, err := client.GetRegistry(ctx, member.RegistryID)
		if err != nil {
			return fmt.Errorf("resolve nested collection registry %q: %w", member.RegistryID, err)
		}
		if nestedRegistry.Acquisition != store.AcquisitionMirror {
			return fmt.Errorf("nested collection %q uses unsupported acquisition %q", member.Name, nestedRegistry.Acquisition)
		}
		nestedRevision, err := client.GetResourceRevisionForRegistry(ctx, string(nestedRegistry.ID), string(nestedRegistry.ID), "collection", member.Name, member.Revision)
		if err != nil {
			return fmt.Errorf("resolve nested collection %q revision: %w", member.Name, err)
		}
		if nestedRevision.ContentDigest != store.RevisionDigest(member.Revision) || nestedRevision.ArchiveDigest != "" {
			return fmt.Errorf("nested collection %q revision metadata does not match manifest", member.Name)
		}
		if err := enforceMarketplaceUse(nestedRevision, policy); err != nil {
			return fmt.Errorf("nested collection %q revision: %w", member.Name, err)
		}
		if _, err := cache.WriteResourceRevision(entry.URL, string(nestedRegistry.ID), "collection", member.Name, member.Revision, nestedRevision); err != nil {
			return fmt.Errorf("cache nested collection %q revision: %w", member.Name, err)
		}
		nested, payload, err := client.GetCollectionManifest(ctx, member.Revision)
		if err != nil {
			return fmt.Errorf("fetch nested collection %q: %w", member.Name, err)
		}
		if nested.Name != member.Name {
			return fmt.Errorf("nested collection name mismatch: got %q, want %q", nested.Name, member.Name)
		}
		if _, err := cache.WriteCollectionManifest(member.Revision, payload); err != nil {
			return fmt.Errorf("cache nested collection %q: %w", member.Name, err)
		}
		if err := cache.MarkCollectionVerified(member.Revision); err != nil {
			return fmt.Errorf("mark nested collection %q manifest verified: %w", member.Name, err)
		}
		nestedManifests[member.Revision] = nested
		visiting[member.Revision] = true
		if err := r.verifyNestedCollectionManifests(ctx, client, cache, entry, nested, visiting, nestedManifests, budget, policy); err != nil {
			return err
		}
		delete(visiting, member.Revision)
	}
	for _, name := range manifest.Includes {
		found := false
		for _, member := range manifest.Members {
			if member.Kind == "collection" && member.Name == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("collection %q includes missing nested collection %q", manifest.Name, name)
		}
	}
	return nil
}

func (r *Resolver) prepareCollectionLeaf(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, collectionRegistry store.Registry, collectionRevision store.AssessedResourceRevision, member hccartifact.CollectionMemberEntry, targets []hcc.Target, policy resolver.MarketplaceRevocationPolicy) (*PreparedInstall, error) {
	memberRegistry, err := client.GetRegistry(ctx, member.RegistryID)
	if err != nil {
		return nil, fmt.Errorf("resolve collection member registry %q: %w", member.RegistryID, err)
	}
	acquisition := config.MarketplaceAcquisitionMirror
	if memberRegistry.Acquisition == store.AcquisitionIndexOnly {
		acquisition = config.MarketplaceAcquisitionIndexOnly
	} else if memberRegistry.Acquisition != store.AcquisitionMirror {
		return nil, fmt.Errorf("collection member %s/%q uses unsupported acquisition %q", member.Kind, member.Name, memberRegistry.Acquisition)
	}
	leafPin := config.MarketplaceResourcePin{
		RegistryID: string(memberRegistry.ID), RegistrySlug: memberRegistry.Slug,
		Kind: member.Kind, Name: member.Name, Revision: member.Revision,
		Digest: member.Revision, Acquisition: acquisition,
		SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: string(collectionRegistry.ID), Name: collectionRevision.Coordinate.Name, Revision: string(collectionRevision.ContentDigest)}}),
	}
	leafRevision, err := client.GetResourceRevisionForRegistry(ctx, string(memberRegistry.ID), string(memberRegistry.ID), member.Kind, member.Name, member.Revision)
	if err != nil {
		return nil, fmt.Errorf("resolve collection member %s/%q: %w", member.Kind, member.Name, err)
	}
	if err := validateResourceRevision(entry, leafPin, leafRevision); err != nil {
		return nil, fmt.Errorf("collection member %s/%q metadata: %w", member.Kind, member.Name, err)
	}
	if err := enforceMarketplaceUse(leafRevision, policy); err != nil {
		return nil, fmt.Errorf("collection member %s/%q: %w", member.Kind, member.Name, err)
	}
	if acquisition == config.MarketplaceAcquisitionIndexOnly {
		if leafRevision.Occurrence.Commit == "" || leafRevision.Occurrence.Coordinate.Registry != memberRegistry.ID {
			return nil, fmt.Errorf("collection member %s/%q has no exact index-only registry occurrence", member.Kind, member.Name)
		}
		occurrence, err := client.GetRegistryOccurrence(ctx, string(memberRegistry.ID), leafRevision.Occurrence.Commit)
		if err != nil {
			return nil, fmt.Errorf("resolve collection member %s/%q occurrence: %w", member.Kind, member.Name, err)
		}
		if occurrence.Acquisition != store.AcquisitionIndexOnly || occurrence.RegistryRevisionManifestDigest != leafRevision.Occurrence.ManifestDigest {
			return nil, fmt.Errorf("collection member %s/%q registry occurrence does not match revision", member.Kind, member.Name)
		}
		leafPin.RegistryCommit = occurrence.Commit
		leafPin.RegistryManifestDigest = string(occurrence.RegistryRevisionManifestDigest)
	}
	resolved, decoded, err := r.resolvePinWithArtifactPolicy(ctx, client, cache, entry, leafPin, targets, resolver.MarketplaceResolveModeRefresh, policy, r.AllowedRegistries)
	if err != nil {
		return nil, fmt.Errorf("verify collection member %s/%q: %w", member.Kind, member.Name, err)
	}
	return &PreparedInstall{Entry: entry, Pin: leafPin, Revision: leafRevision, Resolved: resolved, Artifact: decoded, ArchiveDigest: string(leafRevision.ArchiveDigest)}, nil
}
