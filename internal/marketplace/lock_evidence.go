package marketplace

import (
	"bytes"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

// VerifiedMarketplaceLockResource reconstructs a fully populated Phase 1 lock
// record for one legacy pin from verified local cache evidence. It is
// read-only and offline: no Marketplace API is contacted, no cache entry is
// written, and a currently valid immutable record is not rejected solely for
// carrying a revocation object — migration transfers identity while the normal
// resolver retains live revocation policy.
func (c *Cache) VerifiedMarketplaceLockResource(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) (lockfile.MarketplaceResourceLock, error) {
	canonicalURL, err := urlutil.NormalizeEndpointURL(entry.URL)
	if err != nil {
		return lockfile.MarketplaceResourceLock{}, fmt.Errorf("entry url: %w", err)
	}
	// Cache keys and revision coordinates use the canonical endpoint spelling;
	// an equivalent legacy spelling must resolve the same evidence.
	entry.URL = canonicalURL
	if err := nameutil.ValidateSegment(entry.InstanceID); err != nil {
		return lockfile.MarketplaceResourceLock{}, fmt.Errorf("entry instance_id %s", err)
	}
	acquisition := pin.Acquisition
	if acquisition == "" {
		acquisition = config.MarketplaceAcquisitionMirror
	}
	registryName := pin.RegistrySlug
	if registryName == "" {
		registryName = pin.RegistryID
	}
	record := lockfile.MarketplaceResourceLock{
		RegistryName:  registryName,
		RegistryID:    pin.RegistryID,
		Kind:          pin.Kind,
		Name:          pin.Name,
		Revision:      pin.Revision,
		ContentDigest: lockfile.Digest(pin.Digest),
		Acquisition:   acquisition,
	}
	for _, selection := range pin.SelectedBy.List() {
		if err := nameutil.ValidateSegment(selection.RegistryID); err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("selected_by registry_id %s", err)
		}
		if err := nameutil.ValidateSegment(selection.Name); err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("selected_by name %s", err)
		}
		if _, err := lockfile.ParseDigest(selection.Revision); err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("selected_by revision: %w", err)
		}
		record.SelectedBy = append(record.SelectedBy, lockfile.MarketplaceSelectionLock{
			RegistryID: selection.RegistryID, Name: selection.Name, Revision: lockfile.Digest(selection.Revision),
		})
	}

	identity := indexOnlyIdentity(entry, pin)
	var rev store.AssessedResourceRevision
	var found bool
	if acquisition == config.MarketplaceAcquisitionIndexOnly {
		rev, found, err = c.ReadIndexOnlyResourceRevision(identity, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
	} else {
		rev, found, err = c.ReadResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
	}
	if err != nil {
		return lockfile.MarketplaceResourceLock{}, fmt.Errorf("read cached revision metadata: %w", err)
	}
	if !found {
		return lockfile.MarketplaceResourceLock{}, fmt.Errorf("cached revision metadata for %s/%s/%s is missing", pin.RegistryID, pin.Kind, pin.Name)
	}
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		return lockfile.MarketplaceResourceLock{}, err
	}

	if acquisition == config.MarketplaceAcquisitionIndexOnly {
		if pin.Kind == "collection" {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("index-only collection lock evidence is not supported")
		}
		if rev.Occurrence.Coordinate.Registry != store.RegistryID(pin.RegistryID) ||
			rev.Occurrence.Commit != pin.RegistryCommit ||
			rev.Occurrence.ManifestDigest != store.RevisionDigest(pin.RegistryManifestDigest) {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("cached registry occurrence does not match pinned registry occurrence")
		}
		if rev.Occurrence.SourcePath == "" {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("cached registry occurrence has no source path")
		}
		record.RegistryCommit = pin.RegistryCommit
		record.RegistryManifestDigest = lockfile.Digest(pin.RegistryManifestDigest)
		record.SourcePath = rev.Occurrence.SourcePath
	}

	if pin.Kind == "collection" {
		if acquisition != config.MarketplaceAcquisitionMirror {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("collection acquisition %q is not supported", acquisition)
		}
		raw, verified, err := c.ReadCollectionManifest(pin.Digest)
		if err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("read cached collection manifest: %w", err)
		}
		if raw == nil || !verified {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("verified collection manifest is missing from cache")
		}
		manifest, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(raw))
		if err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("decode cached collection manifest: %w", err)
		}
		if err := hccartifact.VerifyCollectionManifest(manifest, payload); err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("verify cached collection manifest: %w", err)
		}
		if manifest.ContentDigest != pin.Digest {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("collection manifest digest mismatch: got %q, want %q", manifest.ContentDigest, pin.Digest)
		}
		if manifest.Name != pin.Name {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("collection manifest name mismatch: got %q, want %q", manifest.Name, pin.Name)
		}
		record.CollectionManifestDigest = lockfile.Digest(pin.Digest)
	} else {
		if rev.ArchiveDigest == "" {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("cached revision metadata has no archive digest")
		}
		var raw []byte
		var verified bool
		if acquisition == config.MarketplaceAcquisitionIndexOnly {
			raw, verified, err = c.ReadIndexOnlyArtifact(identity, pin.Digest)
		} else {
			raw, verified, err = c.ReadArtifact(string(rev.ArchiveDigest))
		}
		if err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("read cached artifact: %w", err)
		}
		if raw == nil || !verified {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("verified artifact is missing from cache")
		}
		ca, caRaw, err := hccartifact.Decode(bytes.NewReader(raw))
		if err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("decode artifact: %w", err)
		}
		if err := ca.VerifyArchiveDigest(caRaw); err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("verify archive digest: %w", err)
		}
		if err := ca.VerifyContentDigest(); err != nil {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("verify content digest: %w", err)
		}
		if err := validateArtifactManifest(entry, pin, rev, ca); err != nil {
			return lockfile.MarketplaceResourceLock{}, err
		}
		if ca.Manifest.RegistryID != "" && ca.Manifest.RegistryID != pin.RegistryID {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("artifact manifest registry_id mismatch: got %q, want %q", ca.Manifest.RegistryID, pin.RegistryID)
		}
		if ca.Manifest.Revision != "" && ca.Manifest.Revision != pin.Revision {
			return lockfile.MarketplaceResourceLock{}, fmt.Errorf("artifact manifest revision mismatch: got %q, want %q", ca.Manifest.Revision, pin.Revision)
		}
		record.ArchiveDigest = lockfile.Digest(rev.ArchiveDigest)
	}

	// Validate scalar/acquisition/integrity fields through the lock codec on a
	// one-endpoint temporary document. SelectedBy edges target parent
	// collection records that this per-resource method must not fabricate, so
	// the validation copy strips them; cross-resource binding and edge checks
	// belong to the endpoint-level migration planner.
	check := record
	check.SelectedBy = nil
	bindings := []lockfile.MarketplaceRegistryBinding{{Name: record.RegistryName, RegistryID: record.RegistryID}}
	for _, edge := range record.SelectedBy {
		bindings = append(bindings, lockfile.MarketplaceRegistryBinding{Name: edge.RegistryID, RegistryID: edge.RegistryID})
	}
	// SourceUnset has no layer scope; migration supplies the final layer scope
	// separately, so ScopeUser is a harmless temporary codec scope here.
	scope := lockfile.Scope(entry.Source.LockScope())
	if !scope.Valid() {
		scope = lockfile.ScopeUser
	}
	probe := lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      scope,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL:              canonicalURL,
			InstanceID:       entry.InstanceID,
			RegistryBindings: bindings,
			Resources:        []lockfile.MarketplaceResourceLock{check},
		}},
	}
	if err := lockfile.Validate(probe); err != nil {
		return lockfile.MarketplaceResourceLock{}, fmt.Errorf("lock record failed codec validation: %w", err)
	}
	return record, nil
}
