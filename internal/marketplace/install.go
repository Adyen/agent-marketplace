package marketplace

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
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
	Revision      store.ResourceRevision
	Resolved      resolver.ResolvedArtifact
	Artifact      hccartifact.CanonicalArtifact
	ArchiveDigest string

	// CollectionMembers contains the fully verified leaf plans for a virtual
	// collection. The collection itself is never passed to materialization.
	CollectionMembers  []*PreparedInstall
	CollectionManifest hccartifact.CollectionManifest
}

// InstallPreview is the stable, typed preview surface for a prepared install.
type InstallPreview struct {
	MarketplaceAlias     string
	MarketplaceURL       string
	InstanceID           string
	SourceID             string
	SourceSlug           string
	Kind                 string
	Name                 string
	InstalledName        string
	Revision             string
	ContentDigest        string
	ArchiveDigest        string
	Acquisition          string
	SourceCommit         string
	SourceManifestDigest string
	License              string
	Findings             []hccartifact.CanonicalFinding
	Capabilities         []string
	Provenance           hccartifact.CanonicalProvenance
	CollectionMembers    []InstallPreview
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
		SourceID: p.Pin.SourceID, SourceSlug: p.Pin.SourceSlug, Kind: p.Pin.Kind, Name: p.Pin.Name,
		InstalledName: installedName, Revision: p.Pin.Revision, ContentDigest: p.Pin.Digest,
		ArchiveDigest: p.ArchiveDigest, Acquisition: p.Pin.Acquisition,
		SourceCommit: p.Pin.SourceCommit, SourceManifestDigest: p.Pin.SourceManifestDigest,
		License: p.Artifact.Manifest.License, Findings: p.Artifact.Manifest.Findings,
		Capabilities: p.Artifact.Manifest.Capabilities, Provenance: p.Artifact.Manifest.Provenance,
	}
	if p.Pin.Kind == "collection" || len(p.CollectionMembers) > 0 {
		if len(p.Revision.Findings) > 0 {
			preview.Findings = canonicalFindingsFromRevision(p.Revision)
		}
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

func canonicalFindingsFromRevision(revision store.ResourceRevision) []hccartifact.CanonicalFinding {
	if len(revision.Findings) == 0 {
		return nil
	}
	findings := make([]hccartifact.CanonicalFinding, len(revision.Findings))
	for i, finding := range revision.Findings {
		findings[i] = hccartifact.CanonicalFinding{
			Code: finding.Code, Severity: finding.Severity, Message: finding.Message,
			PolicyOutcome: finding.Policy, AnalyzerVersion: revision.AnalyzerVersion,
		}
		file, line, column := parseSourceRange(finding.SourceRange)
		findings[i].File, findings[i].Line, findings[i].Column = file, line, column
	}
	return findings
}

func parseSourceRange(raw string) (file string, line, column int) {
	file = raw
	parts := strings.Split(raw, ":")
	if len(parts) < 2 {
		return file, 0, 0
	}
	last, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return file, 0, 0
	}
	if len(parts) >= 3 {
		second, secondErr := strconv.Atoi(parts[len(parts)-2])
		if secondErr == nil {
			return strings.Join(parts[:len(parts)-2], ":"), second, last
		}
	}
	return strings.Join(parts[:len(parts)-1], ":"), last, 0
}

// LatestRevision verifies the endpoint and source identity, then fetches the
// latest metadata for an existing pin. Index-only pins also validate the exact
// source occurrence, but this method does not fetch an artifact.
func (r *Resolver) LatestRevision(ctx context.Context, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) (store.Source, store.ResourceRevision, error) {
	if err := ctx.Err(); err != nil {
		return store.Source{}, store.ResourceRevision{}, err
	}
	canonical, err := canonicalMarketplaceEntry(entry)
	if err != nil {
		return store.Source{}, store.ResourceRevision{}, err
	}
	client, err := r.clientForEntry(canonical)
	if err != nil {
		return store.Source{}, store.ResourceRevision{}, err
	}
	wellKnown, err := client.WellKnown(ctx)
	if err != nil {
		return store.Source{}, store.ResourceRevision{}, fmt.Errorf("well-known document: %w", err)
	}
	if wellKnown.InstanceID != canonical.InstanceID {
		return store.Source{}, store.ResourceRevision{}, fmt.Errorf("instance ID mismatch: endpoint %q, configured %q", wellKnown.InstanceID, canonical.InstanceID)
	}
	source, err := client.GetSourceBySlug(ctx, marketplaceAPISource(pin))
	if err != nil {
		return store.Source{}, store.ResourceRevision{}, fmt.Errorf("resolve source %q: %w", pin.SourceID, err)
	}
	if source.ID != store.SourceID(pin.SourceID) {
		return store.Source{}, store.ResourceRevision{}, fmt.Errorf("source identity mismatch: got %q, want %q", source.ID, pin.SourceID)
	}
	if source.Acquisition != store.AcquisitionMirror && source.Acquisition != store.AcquisitionIndexOnly {
		return source, store.ResourceRevision{}, fmt.Errorf("source uses unsupported acquisition %q", source.Acquisition)
	}
	resourceSource := source.Slug
	if source.Acquisition == store.AcquisitionIndexOnly {
		resourceSource = string(source.ID)
	}
	if resourceSource == "" {
		resourceSource = string(source.ID)
	}
	latest, err := client.GetLatestResourceRevision(ctx, resourceSource, pin.Kind, pin.Name)
	if err != nil {
		return store.Source{}, store.ResourceRevision{}, fmt.Errorf("resolve latest resource revision: %w", err)
	}
	if err := validateLatestResourceRevision(canonical, pin, latest); err != nil {
		return store.Source{}, store.ResourceRevision{}, err
	}
	if latest.Revocation != nil {
		return store.Source{}, store.ResourceRevision{}, fmt.Errorf("latest revision is revoked")
	}
	if source.Acquisition == store.AcquisitionIndexOnly {
		if latest.Provenance == nil || latest.Provenance.SourceID != source.ID || !mpapi.IsCanonicalGitCommit(latest.Provenance.Commit) || latest.Provenance.ManifestDigest == "" {
			return store.Source{}, store.ResourceRevision{}, fmt.Errorf("latest index-only revision has no exact source occurrence")
		}
		occurrence, err := client.GetSourceOccurrence(ctx, string(source.ID), latest.Provenance.Commit)
		if err != nil {
			return store.Source{}, store.ResourceRevision{}, fmt.Errorf("exact source commit unavailable: %w", err)
		}
		if occurrence.Acquisition != store.AcquisitionIndexOnly || occurrence.SourceRevisionManifestDigest != latest.Provenance.ManifestDigest {
			return store.Source{}, store.ResourceRevision{}, fmt.Errorf("source manifest digest mismatch for latest index-only revision")
		}
		origin := strings.TrimSpace(occurrence.AcquisitionOrigin)
		if err := validateAcquisitionOrigin(origin); err != nil {
			return store.Source{}, store.ResourceRevision{}, fmt.Errorf("policy rejection: invalid acquisition origin: %w", err)
		}
		if err := config.ValidateRegistryURLs([]config.RegistryEntry{{URL: origin, Ref: occurrence.Ref}}, r.AllowedRegistries); err != nil {
			return store.Source{}, store.ResourceRevision{}, fmt.Errorf("policy rejection: acquisition origin rejected by allowed_registries: %w", err)
		}
	}
	return source, latest, nil
}

func validateLocatorRevision(locator MarketplaceLocator, revision store.ResourceRevision) error {
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
	source, err := client.GetSourceBySlug(ctx, locator.Source)
	if err != nil {
		return nil, fmt.Errorf("resolve source %q: %w", locator.Source, err)
	}
	if source.Acquisition != store.AcquisitionMirror && source.Acquisition != store.AcquisitionIndexOnly {
		return nil, fmt.Errorf("source %q uses unsupported acquisition %q", locator.Source, source.Acquisition)
	}

	var revision store.ResourceRevision
	exactRevision := locator.Revision
	if exactRevision == "" {
		exactRevision = locator.Digest
	}
	// Index-only occurrence lookup is keyed by immutable Source ID, while
	// preserving the existing slug route for mirror revision lookups.
	resourceSource := locator.Source
	if source.Acquisition == store.AcquisitionIndexOnly || resourceSource == "" {
		resourceSource = string(source.ID)
	}
	if exactRevision != "" {
		revision, err = client.GetResourceRevisionForSource(ctx, resourceSource, string(source.ID), locator.Kind, locator.Name, exactRevision)
	} else {
		revision, err = client.GetLatestResourceRevisionForSource(ctx, resourceSource, string(source.ID), locator.Kind, locator.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve resource revision: %w", err)
	}
	if exactRevision != "" {
		if err := validateLocatorRevision(locator, revision); err != nil {
			return nil, err
		}
	}
	pinRevision := locator.Revision
	if pinRevision == "" {
		pinRevision = string(revision.ContentDigest)
	}
	pin := config.MarketplaceResourcePin{
		SourceID: string(source.ID), SourceSlug: source.Slug, Kind: locator.Kind,
		Name: locator.Name, Revision: pinRevision, Digest: string(revision.ContentDigest),
		Acquisition: config.MarketplaceAcquisitionMirror,
	}
	if source.Acquisition == store.AcquisitionIndexOnly {
		if revision.Provenance == nil || revision.Provenance.Commit == "" {
			return nil, fmt.Errorf("index-only resource revision has no exact source occurrence")
		}
		occurrence, err := client.GetSourceOccurrence(ctx, string(source.ID), revision.Provenance.Commit)
		if err != nil {
			return nil, fmt.Errorf("resolve exact source occurrence: %w", err)
		}
		if occurrence.Acquisition != store.AcquisitionIndexOnly {
			return nil, fmt.Errorf("resource revision occurrence is not index-only")
		}
		if revision.Provenance.SourceID != source.ID || revision.Provenance.SourceID != occurrence.SourceID {
			return nil, fmt.Errorf("resource revision provenance source ID does not match exact source occurrence")
		}
		if occurrence.SourceRevisionManifestDigest != revision.Provenance.ManifestDigest {
			return nil, fmt.Errorf("resource revision and source occurrence manifest digests differ")
		}
		pin.Acquisition = config.MarketplaceAcquisitionIndexOnly
		pin.SourceCommit = occurrence.Commit
		pin.SourceManifestDigest = string(occurrence.SourceRevisionManifestDigest)
	}

	cache := r.cache()
	if locator.Kind == "collection" {
		if source.Acquisition != store.AcquisitionMirror {
			return nil, fmt.Errorf("collection %q requires mirror acquisition", locator.Name)
		}
		if err := enforceMarketplaceRevocation(revision, nil); err != nil {
			return nil, fmt.Errorf("collection revision: %w", err)
		}
		if _, err := cache.WriteResourceRevision(canonical.URL, pin.SourceID, pin.Kind, pin.Name, pin.Digest, revision); err != nil {
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
		nestedManifests := make(map[string]hccartifact.CollectionManifest)
		if err := r.verifyNestedCollectionManifests(ctx, client, manifest, map[string]bool{string(revision.ContentDigest): true}, nestedManifests); err != nil {
			return nil, err
		}
		leaves, err := hccartifact.ValidateAndExpandCollectionManifestGraph(manifest, nestedManifests)
		if err != nil {
			return nil, fmt.Errorf("validate collection graph: %w", err)
		}
		members := make([]*PreparedInstall, 0, len(leaves))
		seen := make(map[string]string, len(leaves))
		for _, member := range leaves {
			key := member.SourceID + "/" + member.Kind + "/" + member.Name
			if previous, ok := seen[key]; ok {
				if previous != member.Revision {
					return nil, fmt.Errorf("collection contains conflicting revisions for %s", key)
				}
				continue
			}
			seen[key] = member.Revision
			memberPlan, err := r.prepareCollectionLeaf(ctx, client, cache, canonical, source, revision, member, targets)
			if err != nil {
				return nil, err
			}
			members = append(members, memberPlan)
		}
		slog.Info("marketplace collection install plan prepared", "url", canonical.URL, "instance_id", canonical.InstanceID, "source_id", source.ID, "name", locator.Name, "manifest_digest", revision.ContentDigest, "members", len(members))
		return &PreparedInstall{Entry: canonical, Pin: pin, Revision: revision, CollectionManifest: manifest, CollectionMembers: members}, nil
	}
	resolved, artifact, err := r.resolvePinWithArtifact(ctx, client, cache, canonical, pin, targets, resolver.MarketplaceResolveModeRefresh)
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
	slog.Info("marketplace install plan prepared", "url", canonical.URL, "instance_id", canonical.InstanceID, "source_id", source.ID, "kind", locator.Kind, "name", locator.Name, "digest", pin.Digest, "acquisition", pin.Acquisition, "source_commit", pin.SourceCommit)
	return &PreparedInstall{Entry: canonical, Pin: pin, Revision: revision, Resolved: resolved, Artifact: artifact, ArchiveDigest: archiveDigest}, nil
}

func (r *Resolver) verifyNestedCollectionManifests(ctx context.Context, client *Client, manifest hccartifact.CollectionManifest, visiting map[string]bool, nestedManifests map[string]hccartifact.CollectionManifest) error {
	for _, member := range manifest.Members {
		if member.Kind != "collection" {
			continue
		}
		if member.SourceID == "" || member.Revision == "" {
			return fmt.Errorf("collection %q has an unresolved nested member %q", manifest.Name, member.Name)
		}
		if visiting[member.Revision] {
			return fmt.Errorf("collection cycle detected at %q", member.Name)
		}
		nestedSource, err := client.GetSource(ctx, member.SourceID)
		if err != nil {
			return fmt.Errorf("resolve nested collection source %q: %w", member.SourceID, err)
		}
		if nestedSource.Acquisition != store.AcquisitionMirror {
			return fmt.Errorf("nested collection %q uses unsupported acquisition %q", member.Name, nestedSource.Acquisition)
		}
		nestedRevision, err := client.GetResourceRevisionForSource(ctx, string(nestedSource.ID), string(nestedSource.ID), "collection", member.Name, member.Revision)
		if err != nil {
			return fmt.Errorf("resolve nested collection %q revision: %w", member.Name, err)
		}
		if nestedRevision.ContentDigest != store.RevisionDigest(member.Revision) || nestedRevision.ArchiveDigest != "" {
			return fmt.Errorf("nested collection %q revision metadata does not match manifest", member.Name)
		}
		if err := enforceMarketplaceRevocation(nestedRevision, nil); err != nil {
			return fmt.Errorf("nested collection %q revision: %w", member.Name, err)
		}
		nested, payload, err := client.GetCollectionManifest(ctx, member.Revision)
		if err != nil {
			return fmt.Errorf("fetch nested collection %q: %w", member.Name, err)
		}
		if nested.Name != member.Name {
			return fmt.Errorf("nested collection name mismatch: got %q, want %q", nested.Name, member.Name)
		}
		if _, err := r.cache().WriteCollectionManifest(member.Revision, payload); err != nil {
			return fmt.Errorf("cache nested collection %q: %w", member.Name, err)
		}
		nestedManifests[member.Revision] = nested
		visiting[member.Revision] = true
		if err := r.verifyNestedCollectionManifests(ctx, client, nested, visiting, nestedManifests); err != nil {
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

func (r *Resolver) prepareCollectionLeaf(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, collectionSource store.Source, collectionRevision store.ResourceRevision, member hccartifact.CollectionMemberEntry, targets []hcc.Target) (*PreparedInstall, error) {
	memberSource, err := client.GetSource(ctx, member.SourceID)
	if err != nil {
		return nil, fmt.Errorf("resolve collection member source %q: %w", member.SourceID, err)
	}
	acquisition := config.MarketplaceAcquisitionMirror
	if memberSource.Acquisition == store.AcquisitionIndexOnly {
		acquisition = config.MarketplaceAcquisitionIndexOnly
	} else if memberSource.Acquisition != store.AcquisitionMirror {
		return nil, fmt.Errorf("collection member %s/%q uses unsupported acquisition %q", member.Kind, member.Name, memberSource.Acquisition)
	}
	leafPin := config.MarketplaceResourcePin{
		SourceID: string(memberSource.ID), SourceSlug: memberSource.Slug,
		Kind: member.Kind, Name: member.Name, Revision: member.Revision,
		Digest: member.Revision, Acquisition: acquisition,
		SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{SourceID: string(collectionSource.ID), Name: collectionRevision.Coordinate.Name, Revision: string(collectionRevision.ContentDigest)}}),
	}
	leafRevision, err := client.GetResourceRevisionForSource(ctx, string(memberSource.ID), string(memberSource.ID), member.Kind, member.Name, member.Revision)
	if err != nil {
		return nil, fmt.Errorf("resolve collection member %s/%q: %w", member.Kind, member.Name, err)
	}
	if err := validateResourceRevision(entry, leafPin, leafRevision); err != nil {
		return nil, fmt.Errorf("collection member %s/%q metadata: %w", member.Kind, member.Name, err)
	}
	if acquisition == config.MarketplaceAcquisitionIndexOnly {
		if leafRevision.Provenance == nil || leafRevision.Provenance.Commit == "" || leafRevision.Provenance.SourceID != memberSource.ID {
			return nil, fmt.Errorf("collection member %s/%q has no exact index-only source occurrence", member.Kind, member.Name)
		}
		occurrence, err := client.GetSourceOccurrence(ctx, string(memberSource.ID), leafRevision.Provenance.Commit)
		if err != nil {
			return nil, fmt.Errorf("resolve collection member %s/%q occurrence: %w", member.Kind, member.Name, err)
		}
		if occurrence.Acquisition != store.AcquisitionIndexOnly || occurrence.SourceRevisionManifestDigest != leafRevision.Provenance.ManifestDigest {
			return nil, fmt.Errorf("collection member %s/%q source occurrence does not match revision", member.Kind, member.Name)
		}
		leafPin.SourceCommit = occurrence.Commit
		leafPin.SourceManifestDigest = string(occurrence.SourceRevisionManifestDigest)
	}
	resolved, decoded, err := r.resolvePinWithArtifact(ctx, client, cache, entry, leafPin, targets, resolver.MarketplaceResolveModeRefresh)
	if err != nil {
		return nil, fmt.Errorf("verify collection member %s/%q: %w", member.Kind, member.Name, err)
	}
	return &PreparedInstall{Entry: entry, Pin: leafPin, Revision: leafRevision, Resolved: resolved, Artifact: decoded, ArchiveDigest: string(leafRevision.ArchiveDigest)}, nil
}
