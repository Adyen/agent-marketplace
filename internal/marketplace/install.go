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
}

// InstallPreview is the stable, typed preview surface for a prepared install.
type InstallPreview struct {
	MarketplaceAlias string
	MarketplaceURL   string
	InstanceID       string
	SourceID         string
	SourceSlug       string
	Kind             string
	Name             string
	InstalledName    string
	Revision         string
	ContentDigest    string
	ArchiveDigest    string
	Acquisition      string
	License          string
	Findings         []hccartifact.CanonicalFinding
	Capabilities     []string
	Provenance       hccartifact.CanonicalProvenance
}

// Preview returns metadata decoded from the verified artifact, not from an
// untrusted index response.
func (p *PreparedInstall) Preview() InstallPreview {
	installedName := p.Pin.InstalledName
	if installedName == "" {
		installedName = p.Pin.Name
	}
	return InstallPreview{
		MarketplaceAlias: p.Entry.Alias, MarketplaceURL: p.Entry.URL, InstanceID: p.Entry.InstanceID,
		SourceID: p.Pin.SourceID, SourceSlug: p.Pin.SourceSlug, Kind: p.Pin.Kind, Name: p.Pin.Name,
		InstalledName: installedName, Revision: p.Pin.Revision, ContentDigest: p.Pin.Digest,
		ArchiveDigest: p.ArchiveDigest, Acquisition: config.MarketplaceAcquisitionMirror,
		License: p.Artifact.Manifest.License, Findings: p.Artifact.Manifest.Findings,
		Capabilities: p.Artifact.Manifest.Capabilities, Provenance: p.Artifact.Manifest.Provenance,
	}
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

	if locator.Kind == "collection" {
		return nil, fmt.Errorf("collection Marketplace install is not supported yet; install an individual resource")
	}
	if locator.Kind == "" {
		return nil, fmt.Errorf("marketplace resource kind is required")
	}
	source, err := client.GetSource(ctx, locator.Source)
	if err != nil {
		return nil, fmt.Errorf("resolve source %q: %w", locator.Source, err)
	}
	if source.Acquisition != store.AcquisitionMirror {
		return nil, fmt.Errorf("source %q uses %q acquisition; this install supports mirror only (index-only is not supported)", locator.Source, source.Acquisition)
	}

	var revision store.ResourceRevision
	if locator.Revision != "" {
		revision, err = client.GetResourceRevision(ctx, string(source.ID), locator.Kind, locator.Name, locator.Revision)
	} else {
		revision, err = client.GetLatestResourceRevision(ctx, string(source.ID), locator.Kind, locator.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve resource revision: %w", err)
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
	cache := r.cache()
	resolved, artifact, err := r.resolvePinWithArtifact(ctx, client, cache, canonical, pin, targets, resolver.MarketplaceResolveModeRefresh)
	if err != nil {
		return nil, fmt.Errorf("verify Marketplace artifact: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slog.Info("marketplace install plan prepared", "url", canonical.URL, "instance_id", canonical.InstanceID, "source_id", source.ID, "kind", locator.Kind, "name", locator.Name, "digest", pin.Digest)
	return &PreparedInstall{Entry: canonical, Pin: pin, Revision: revision, Resolved: resolved, Artifact: artifact, ArchiveDigest: string(revision.ArchiveDigest)}, nil
}
