package marketplace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type PinState string

const (
	PinStateCurrent         PinState = "current"
	PinStateCached          PinState = "cached"
	PinStateMissing         PinState = "missing"
	PinStateRevoked         PinState = "revoked"
	PinStateUnavailable     PinState = "unavailable"
	PinStateStale           PinState = "stale"
	PinStateUpdateAvailable PinState = "update-available"
)

type CacheState string

const (
	CacheStateNotApplicable      CacheState = "not-applicable"
	CacheStateMissing            CacheState = "missing"
	CacheStateMetadataOnly       CacheState = "metadata-only"
	CacheStateArtifactUnverified CacheState = "artifact-unverified"
	CacheStateArtifactVerified   CacheState = "artifact-verified"
	CacheStateStale              CacheState = "stale"
	CacheStateError              CacheState = "error"
)

type AcquisitionMode string

const (
	AcquisitionMirror    AcquisitionMode = config.MarketplaceAcquisitionMirror
	AcquisitionIndexOnly AcquisitionMode = config.MarketplaceAcquisitionIndexOnly
)

type PinStatus struct {
	Alias            string
	URL              string
	InstanceID       string
	RegistryID       string
	RegistrySlug     string
	Kind             string
	Name             string
	InstalledName    string
	Revision         string
	ContentDigest    string
	ArchiveDigest    string
	Acquisition      AcquisitionMode
	Source           config.Source
	Required         bool
	State            PinState
	Cache            CacheState
	MetadataCached   bool
	ArtifactPresent  bool
	ArtifactVerified bool
	EndpointQueried  bool
	LatestQueried    bool
	UpdateAvailable  bool
	Detail           string
}

type Inspection struct {
	Pins      []PinStatus
	Artifacts []resolver.ResolvedArtifact
	Err       error
}

// PinStatusError promotes an unhealthy required Marketplace pin to a pipeline
// error while leaving the complete PinStatus available to status rendering.
type PinStatusError struct {
	Pin PinStatus
}

func (e *PinStatusError) Error() string {
	registryID := QuoteTerminal(e.Pin.RegistryID)
	kind := QuoteTerminal(e.Pin.Kind)
	name := QuoteTerminal(e.Pin.Name)
	state := QuoteTerminal(string(e.Pin.State))
	if e.Pin.Detail == "" {
		return fmt.Sprintf("required Marketplace pin %s/%s/%s is %s", registryID, kind, name, state)
	}
	return fmt.Sprintf("required Marketplace pin %s/%s/%s is %s: %s", registryID, kind, name, state, QuoteTerminal(e.Pin.Detail))
}

func (i Inspection) Resolve(ctx context.Context, _ *config.Config, _ []hcc.Target, _ resolver.MarketplaceResolveMode) ([]resolver.ResolvedArtifact, []error) {
	if i.Err != nil {
		return nil, []error{i.Err}
	}
	if err := ctx.Err(); err != nil {
		return nil, []error{err}
	}

	var errs []error
	errs = append(errs, RequiredPinHealthErrors(i.Pins)...)

	// Inspection artifacts are intentionally metadata-only. Do not let them
	// enter winner selection, collision reporting, requirement validation, or
	// materialization. Keep this filter defensive for callers that construct an
	// Inspection with a mix of metadata and fully materializable artifacts.
	artifacts := make([]resolver.ResolvedArtifact, 0, len(i.Artifacts))
	for _, artifact := range i.Artifacts {
		if artifact.Unresolved || artifact.Revoked || artifact.Kind == ir.KindCollection || artifact.Resource == nil {
			continue
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, errs
}

func pinStatusUnhealthy(pin PinStatus) bool {
	return pin.Cache == CacheStateError || pin.State == PinStateRevoked || pin.State == PinStateMissing || pin.State == PinStateUnavailable
}

// RequiredPinHealthErrors returns one error per required pin whose inspected
// state is unhealthy. Read-only surfaces that also resolve Marketplace
// artifacts from the verified local cache merge these with resolution
// failures so one broken pin produces one attributable error rather than two
// rows for the same pin.
func RequiredPinHealthErrors(pins []PinStatus) []error {
	var errs []error
	for _, pin := range pins {
		if pin.Required && pinStatusUnhealthy(pin) {
			errs = append(errs, &PinStatusError{Pin: pin})
		}
	}
	return errs
}

func (r *Resolver) Inspect(ctx context.Context, cfg *config.Config, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode) Inspection {
	if cfg == nil || len(cfg.Marketplaces) == 0 {
		return Inspection{}
	}

	start := time.Now()
	slog.Debug("marketplace status inspection started", "marketplaces", len(cfg.Marketplaces), "mode", marketplaceResolveModeName(mode))
	cache := r.cache()
	allowedRegistries := r.AllowedRegistries
	if cfg.AllowedRegistries != nil {
		allowedRegistries = *cfg.AllowedRegistries
	}
	var inspection Inspection
	for _, configuredEntry := range cfg.Marketplaces {
		entry, entryErr := canonicalMarketplaceEntry(configuredEntry)
		if entryErr != nil {
			for _, pin := range configuredEntry.Resources {
				status, artifact, ok := newPinStatus(configuredEntry, pin, targetSet)
				if !ok {
					continue
				}
				status.State = PinStateUnavailable
				status.Detail = entryErr.Error()
				inspection.Pins = append(inspection.Pins, status)
				inspection.Artifacts = append(inspection.Artifacts, artifact)
			}
			continue
		}

		var client *Client
		if mode == resolver.MarketplaceResolveModeCachedNoFetch {
			slog.Debug("marketplace status inspection using cached state only", "url", entry.URL, "instance_id", entry.InstanceID)
		} else {
			var clientErr error
			client, clientErr = r.clientForEntry(entry)
			if clientErr != nil {
				slog.Warn("marketplace status endpoint unavailable", "url", entry.URL, "error", clientErr)
				inspection.appendUnavailable(cache, entry, targetSet, clientErr)
				continue
			}

			endpointErr := error(nil)
			if err := ctx.Err(); err != nil {
				endpointErr = err
			} else {
				wellKnown, err := client.WellKnown(ctx)
				if err != nil {
					endpointErr = err
				} else if wellKnown.InstanceID != entry.InstanceID {
					endpointErr = fmt.Errorf("instance ID mismatch: endpoint %q, configured %q", wellKnown.InstanceID, entry.InstanceID)
				}
			}
			if endpointErr != nil {
				slog.Warn("marketplace status endpoint unavailable", "url", entry.URL, "error", endpointErr)
				inspection.appendUnavailable(cache, entry, targetSet, endpointErr)
				continue
			}
		}

		for _, pin := range entry.Resources {
			status, artifact, ok := newPinStatus(entry, pin, targetSet)
			if !ok {
				continue
			}
			if status.State == PinStateUnavailable && status.Detail != "" {
				inspection.Pins = append(inspection.Pins, status)
				inspection.Artifacts = append(inspection.Artifacts, artifact)
				logPinDecision(status)
				continue
			}
			cached, metadataCached, cacheErr := readCachedPin(cache, entry, pin)
			status.MetadataCached = metadataCached
			if cacheErr != nil {
				status.Cache = CacheStateError
				status.State = PinStateUnavailable
				status.Detail = cacheErr.Error()
			} else if pin.Acquisition == config.MarketplaceAcquisitionIndexOnly && metadataCached {
				if err := validateCachedIndexOnlyRevision(entry, pin, cached); err != nil {
					status.Cache = CacheStateError
					status.State = PinStateUnavailable
					status.Detail = "digest mismatch: " + err.Error()
				} else {
					populateCacheFacts(&status, cache, entry, pin, cached)
				}
			} else {
				populateCacheFacts(&status, cache, entry, pin, cached)
			}
			validateCachedIndexOnlyStagingFacts(cache, entry, pin, &status)

			if mode == resolver.MarketplaceResolveModeCached || mode == resolver.MarketplaceResolveModeCachedNoFetch {
				applyCachedState(&status, cached, metadataCached, cacheErr)
				inspection.Pins = append(inspection.Pins, status)
				inspection.Artifacts = append(inspection.Artifacts, artifact)
				logPinDecision(status)
				continue
			}

			if err := ctx.Err(); err != nil {
				status.State = PinStateUnavailable
				appendPinDetail(&status, err.Error())
			} else {
				status.EndpointQueried = true
				remote, err := client.GetResourceRevision(ctx, marketplaceAPIRegistry(pin), pin.Kind, pin.Name, pin.Digest)
				if err != nil {
					applyRemoteError(&status, cached, metadataCached, cacheErr, err)
				} else if err := validateResourceRevision(entry, pin, remote); err != nil {
					status.State = PinStateUnavailable
					appendPinDetail(&status, err.Error())
				} else if err := enforceMarketplaceAssessment(remote); err != nil {
					status.State = PinStateUnavailable
					appendPinDetail(&status, err.Error())
				} else {
					applyRevision(&status, remote)
					status.ArtifactPresent = false
					status.ArtifactVerified = false
					if cacheErr == nil {
						populateCacheFacts(&status, cache, entry, pin, remote)
					}
					if pin.Acquisition == config.MarketplaceAcquisitionIndexOnly {
						identity := IndexOnlyIdentity{Marketplace: entry.InstanceID, RegistryID: pin.RegistryID, Commit: pin.RegistryCommit, ManifestDigest: pin.RegistryManifestDigest}
						if _, cacheWriteErr := cache.WriteIndexOnlyResourceRevision(identity, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, remote); cacheWriteErr != nil {
							appendPinDetail(&status, "cache write failed: "+cacheWriteErr.Error())
						}
					}
					artifact.Revoked = remote.Revocation != nil
					if remote.Revocation != nil {
						status.State = PinStateRevoked
					} else if status.Cache != CacheStateError {
						status.State = PinStateCurrent
					} else {
						status.State = PinStateUnavailable
					}
					if pin.Acquisition == config.MarketplaceAcquisitionIndexOnly {
						inspectIndexOnlyOccurrence(ctx, client, entry, pin, &status, allowedRegistries)
						if status.State == PinStateCurrent {
							queryLatestRevision(ctx, client, entry, pin, &status)
						}
					} else {
						queryLatestRevision(ctx, client, entry, pin, &status)
					}
				}
			}
			inspection.Pins = append(inspection.Pins, status)
			inspection.Artifacts = append(inspection.Artifacts, artifact)
			logPinDecision(status)
		}
	}

	slices.SortFunc(inspection.Pins, comparePinStatus)
	slices.SortFunc(inspection.Artifacts, func(a, b resolver.ResolvedArtifact) int {
		if c := strings.Compare(a.Kind.String(), b.Kind.String()); c != 0 {
			return c
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.RegistryURL, b.RegistryURL)
	})
	inspection.Err = ctx.Err()
	slog.Debug("marketplace status inspection completed", "pins", len(inspection.Pins), "duration_ms", time.Since(start).Milliseconds())
	return inspection
}

func (i *Inspection) appendUnavailable(cache *Cache, entry config.MarketplaceEntry, targetSet []hcc.Target, err error) {
	for _, pin := range entry.Resources {
		status, artifact, ok := newPinStatus(entry, pin, targetSet)
		if !ok {
			continue
		}
		if status.State == PinStateUnavailable && status.Detail != "" {
			appendPinDetail(&status, err.Error())
			i.Pins = append(i.Pins, status)
			i.Artifacts = append(i.Artifacts, artifact)
			logPinDecision(status)
			continue
		}
		cached, metadataCached, cacheErr := readCachedPin(cache, entry, pin)
		status.MetadataCached = metadataCached
		if cacheErr != nil {
			status.Cache = CacheStateError
			status.State = PinStateUnavailable
			status.Detail = cacheErr.Error()
		} else {
			populateCacheFacts(&status, cache, entry, pin, cached)
		}
		validateCachedIndexOnlyStagingFacts(cache, entry, pin, &status)
		if status.Cache != CacheStateError && cacheErr == nil && cached.Revocation != nil {
			applyRevision(&status, cached)
			artifact.Revoked = true
			status.State = PinStateRevoked
		} else {
			status.State = PinStateUnavailable
		}
		appendPinDetail(&status, err.Error())
		i.Pins = append(i.Pins, status)
		i.Artifacts = append(i.Artifacts, artifact)
		logPinDecision(status)
	}
}

func newPinStatus(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, targetSet []hcc.Target) (PinStatus, resolver.ResolvedArtifact, bool) {
	kind, kindErr := irKind(pin.Kind)
	acquisition := AcquisitionMode(pin.Acquisition)
	if acquisition == "" {
		acquisition = AcquisitionMirror
	}
	required := marketplacePinRequired(entry)
	name := pin.Name
	if pin.InstalledName != "" {
		name = pin.InstalledName
	}
	status := PinStatus{
		Alias: entry.Alias, URL: entry.URL, InstanceID: entry.InstanceID,
		RegistryID: pin.RegistryID, RegistrySlug: pin.RegistrySlug, Kind: pin.Kind,
		Name: pin.Name, InstalledName: pin.InstalledName, Revision: pin.Revision,
		ContentDigest: pin.Digest, Acquisition: acquisition, Source: entry.Source,
		Required: required,
	}
	if kindErr != nil {
		status.State = PinStateUnavailable
		status.Detail = kindErr.Error()
	}
	artifact := resolver.ResolvedArtifact{
		Entry: catalog.Entry{Kind: kind, Name: name}, RegistryURL: entry.URL,
		RegistryPath: pin.RegistryID, Ref: pin.Revision, Source: entry.Source,
		Required: required, CatalogKey: marketplaceCatalogKey(entry, pin),
		Harnesses: targetSet, IsMarketplace: true, MarketplaceURL: entry.URL,
		MarketplaceInstanceID: entry.InstanceID, MarketplaceAlias: entry.Alias,
		MarketplaceRegistryID: pin.RegistryID, MarketplaceResourceName: pin.Name, InstalledName: pin.InstalledName,
		Unresolved: true,
	}
	return status, artifact, true
}

func readCachedPin(cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) (store.AssessedResourceRevision, bool, error) {
	if cache == nil {
		return store.AssessedResourceRevision{}, false, errors.New("marketplace cache is unavailable")
	}
	if pin.Acquisition == config.MarketplaceAcquisitionIndexOnly {
		identity := IndexOnlyIdentity{Marketplace: entry.InstanceID, RegistryID: pin.RegistryID, Commit: pin.RegistryCommit, ManifestDigest: pin.RegistryManifestDigest}
		return cache.ReadIndexOnlyResourceRevision(identity, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
	}
	return cache.ReadResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
}

func populateCacheFacts(status *PinStatus, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, rev store.AssessedResourceRevision) {
	// A cache read/decode error is a fact in its own right. Never replace it
	// with a missing or metadata-only classification, nor let a later successful
	// artifact read make malformed metadata appear healthy.
	if status.Cache == CacheStateError {
		return
	}
	if !status.MetadataCached {
		status.Cache = CacheStateMissing
	}
	if rev.ArchiveDigest == "" && status.Acquisition != AcquisitionIndexOnly {
		if status.MetadataCached {
			status.Cache = CacheStateMetadataOnly
		}
		return
	}
	status.ArchiveDigest = string(rev.ArchiveDigest)
	if cache == nil {
		status.Cache = CacheStateError
		return
	}
	var raw []byte
	var verified bool
	var err error
	if status.Acquisition == AcquisitionIndexOnly {
		identity := IndexOnlyIdentity{Marketplace: entry.InstanceID, RegistryID: pin.RegistryID, Commit: pin.RegistryCommit, ManifestDigest: pin.RegistryManifestDigest}
		raw, verified, err = cache.ReadIndexOnlyArtifact(identity, pin.Digest)
	} else {
		raw, verified, err = cache.ReadArtifact(string(rev.ArchiveDigest))
	}
	if err != nil {
		status.Cache = CacheStateError
		status.Detail = err.Error()
		return
	}
	if status.Acquisition == AcquisitionIndexOnly && raw != nil {
		if err := validateCachedIndexOnlyArtifact(raw, pin); err != nil {
			status.Cache = CacheStateError
			status.Detail = "digest mismatch: " + err.Error()
			return
		}
	}
	status.ArtifactPresent = raw != nil
	status.ArtifactVerified = verified
	if verified {
		status.Cache = CacheStateArtifactVerified
	} else if raw != nil {
		status.Cache = CacheStateArtifactUnverified
	} else {
		status.Cache = CacheStateMissing
	}
	metadataPath := cache.MetadataPath(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
	if pin.Acquisition == config.MarketplaceAcquisitionIndexOnly {
		identity := IndexOnlyIdentity{Marketplace: entry.InstanceID, RegistryID: pin.RegistryID, Commit: pin.RegistryCommit, ManifestDigest: pin.RegistryManifestDigest}
		metadataPath = cache.IndexOnlyMetadataPath(identity, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
	}
	if status.MetadataCached && cacheIsStale(metadataPath) {
		status.Cache = CacheStateStale
	}
}

func validateCachedIndexOnlyRevision(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, rev store.AssessedResourceRevision) error {
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		return err
	}
	if rev.Occurrence.Coordinate.Registry != store.RegistryID(pin.RegistryID) || rev.Occurrence.Commit != pin.RegistryCommit || rev.Occurrence.ManifestDigest != store.RevisionDigest(pin.RegistryManifestDigest) {
		return errors.New("cached resource occurrence does not match pinned registry occurrence")
	}
	return nil
}

func validateCachedIndexOnlyArtifact(raw []byte, pin config.MarketplaceResourcePin) error {
	// The local reconstruction's own archive digest is not attested here; the
	// occurrence's registry manifest binds it, so only content is checked.
	artifact, err := hccartifact.DecodeContentArchive(raw)
	if err != nil {
		return fmt.Errorf("validate artifact: %w", err)
	}
	if err := artifact.VerifyContentDigest(); err != nil {
		return fmt.Errorf("verify content: %w", err)
	}
	if artifact.Manifest.Kind != pin.Kind || artifact.Manifest.Name != pin.Name || artifact.Manifest.ContentDigest != pin.Digest {
		return fmt.Errorf("artifact identity does not match pin")
	}
	return nil
}

func validateCachedIndexOnlyStagingFacts(cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, status *PinStatus) {
	if cache == nil || pin.Acquisition != config.MarketplaceAcquisitionIndexOnly || !status.MetadataCached || !status.ArtifactPresent || status.Cache == CacheStateError {
		return
	}
	identity := indexOnlyIdentity(entry, pin)
	resourcePath := cachedReconstructedResourcePath(cache, identity, pin.Kind, pin.Name)
	if err := validateCachedReconstructedResource(cache, resourcePath); err != nil {
		status.Cache = CacheStateError
		status.State = PinStateUnavailable
		status.Detail = fmt.Sprintf("cached reconstructed source: validate occurrence-scoped reconstructed source %q: %v", resourcePath, err)
		cache.InvalidateIndexOnlyArtifactVerified(identity, pin.Digest)
	}
}

func applyCachedState(status *PinStatus, rev store.AssessedResourceRevision, metadataCached bool, cacheErr error) {
	if cacheErr != nil || status.Cache == CacheStateError {
		status.State = PinStateUnavailable
		return
	}
	if !metadataCached {
		status.State = PinStateMissing
		return
	}
	applyRevision(status, rev)
	if err := enforceMarketplaceAssessment(rev); err != nil {
		status.State = PinStateUnavailable
		appendPinDetail(status, err.Error())
	} else if rev.Revocation != nil {
		status.State = PinStateRevoked
	} else if status.Cache == CacheStateStale {
		status.State = PinStateStale
	} else if status.ArtifactVerified {
		status.State = PinStateCached
	} else {
		status.State = PinStateMissing
	}
}

func applyRevision(status *PinStatus, rev store.AssessedResourceRevision) {
	status.ContentDigest = string(rev.ContentDigest)
	status.ArchiveDigest = string(rev.ArchiveDigest)
}

func applyRemoteError(status *PinStatus, cached store.AssessedResourceRevision, metadataCached bool, cacheErr error, err error) {
	if cacheErr == nil && metadataCached && cached.Revocation != nil {
		applyRevision(status, cached)
		status.State = PinStateRevoked
		appendPinDetail(status, err.Error())
		return
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
		status.State = PinStateMissing
		appendPinDetail(status, err.Error())
		return
	}
	status.State = PinStateUnavailable
	appendPinDetail(status, err.Error())
}

func marketplaceAPIRegistry(pin config.MarketplaceResourcePin) string {
	if pin.RegistrySlug != "" {
		return pin.RegistrySlug
	}
	return pin.RegistryID
}

func inspectIndexOnlyOccurrence(ctx context.Context, client *Client, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, status *PinStatus, allowedRegistries []string) {
	if client == nil {
		status.State = PinStateUnavailable
		appendPinDetail(status, "exact registry commit unavailable: Marketplace client is unavailable")
		return
	}
	if err := ctx.Err(); err != nil {
		status.State = PinStateUnavailable
		appendPinDetail(status, err.Error())
		return
	}
	status.EndpointQueried = true
	occ, err := client.GetRegistryOccurrence(ctx, pin.RegistryID, pin.RegistryCommit)
	if err != nil {
		status.State = PinStateUnavailable
		appendPinDetail(status, "exact registry commit unavailable: "+err.Error())
		return
	}
	if occ.RegistryID != store.RegistryID(pin.RegistryID) || occ.Commit != pin.RegistryCommit || occ.Acquisition != store.AcquisitionIndexOnly {
		status.State = PinStateUnavailable
		appendPinDetail(status, "exact registry occurrence identity mismatch")
		return
	}
	if occ.RegistryRevisionManifestDigest != store.RevisionDigest(pin.RegistryManifestDigest) {
		status.State = PinStateUnavailable
		appendPinDetail(status, fmt.Sprintf("registry manifest digest mismatch: occurrence %q, pin %q", occ.RegistryRevisionManifestDigest, pin.RegistryManifestDigest))
		return
	}
	origin := strings.TrimSpace(occ.AcquisitionOrigin)
	if err := validateAcquisitionOrigin(origin); err != nil {
		status.State = PinStateUnavailable
		appendPinDetail(status, "policy rejection: invalid acquisition origin: "+err.Error())
		return
	}
	if err := config.ValidateRegistryURLs([]config.RegistryEntry{{URL: origin, Ref: occ.Ref}}, allowedRegistries); err != nil {
		status.State = PinStateUnavailable
		appendPinDetail(status, "policy rejection: acquisition origin rejected by allowed_registries: "+err.Error())
		return
	}
}

func IndexOnlyOccurrenceChanged(pin config.MarketplaceResourcePin, latest store.AssessedResourceRevision) bool {
	if pin.Acquisition != config.MarketplaceAcquisitionIndexOnly {
		return false
	}
	return latest.Occurrence.Commit != pin.RegistryCommit || latest.Occurrence.ManifestDigest != store.RevisionDigest(pin.RegistryManifestDigest)
}

func queryLatestRevision(ctx context.Context, client *Client, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, status *PinStatus) {
	if err := ctx.Err(); err != nil {
		appendPinDetail(status, err.Error())
		return
	}
	latest, err := client.GetLatestResourceRevision(ctx, marketplaceAPIRegistry(pin), pin.Kind, pin.Name)
	if err != nil {
		// Latest is observational. An exact pin that was successfully queried
		// keeps its state, and LatestQueried remains false when this probe fails.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			appendPinDetail(status, err.Error())
		}
		return
	}
	if err := validateLatestResourceRevision(entry, pin, latest); err != nil {
		return
	}
	status.LatestQueried = true
	if err := enforceMarketplaceAssessment(latest); err != nil {
		appendPinDetail(status, "latest revision assessment is rejected")
		return
	}
	if latest.Revocation != nil {
		appendPinDetail(status, "latest revision is revoked")
		return
	}
	status.UpdateAvailable = latest.ContentDigest != store.RevisionDigest(pin.Digest) || IndexOnlyOccurrenceChanged(pin, latest)
	if status.UpdateAvailable && status.State == PinStateCurrent {
		status.State = PinStateUpdateAvailable
	}
}

func validateLatestResourceRevision(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, rev store.AssessedResourceRevision) error {
	if err := validateAPIResourceRevision(rev); err != nil {
		return fmt.Errorf("invalid latest assessed resource revision: %w", err)
	}
	want := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID(entry.InstanceID),
		Registry:    store.RegistryID(pin.RegistryID),
		Kind:        pin.Kind,
		Name:        pin.Name,
	}
	if rev.Coordinate != want {
		return fmt.Errorf("latest resource coordinate mismatch: got %q, want %q", rev.Coordinate, want)
	}
	if err := validateSHA256Digest(string(rev.ContentDigest)); err != nil {
		return fmt.Errorf("latest revision content digest %q: %w", rev.ContentDigest, err)
	}
	if rev.ArchiveDigest != "" {
		if err := validateSHA256Digest(string(rev.ArchiveDigest)); err != nil {
			return fmt.Errorf("latest revision archive digest %q: %w", rev.ArchiveDigest, err)
		}
	} else if rev.Revocation == nil && pin.Kind != "collection" && pin.Acquisition != config.MarketplaceAcquisitionIndexOnly {
		return errors.New("latest revision archive digest is required")
	}
	return nil
}

func appendPinDetail(status *PinStatus, detail string) {
	if detail == "" || strings.Contains(status.Detail, detail) {
		return
	}
	if status.Detail == "" {
		status.Detail = detail
		return
	}
	status.Detail += "; " + detail
}

func cacheIsStale(path string) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) > 24*time.Hour
}

func comparePinStatus(a, b PinStatus) int {
	for _, pair := range [][2]string{{a.Kind, b.Kind}, {a.Name, b.Name}, {a.URL, b.URL}, {a.RegistryID, b.RegistryID}, {a.Revision, b.Revision}} {
		if c := strings.Compare(pair[0], pair[1]); c != 0 {
			return c
		}
	}
	return strings.Compare(a.ContentDigest, b.ContentDigest)
}

func marketplaceResolveModeName(mode resolver.MarketplaceResolveMode) string {
	switch mode {
	case resolver.MarketplaceResolveModeRefresh:
		return "refresh"
	case resolver.MarketplaceResolveModeCached:
		return "cached"
	case resolver.MarketplaceResolveModeInspect:
		return "inspect"
	case resolver.MarketplaceResolveModeCachedNoFetch:
		return "cached-no-fetch"
	default:
		return "unknown"
	}
}

func logPinDecision(status PinStatus) {
	slog.Debug("marketplace status pin decision", "url", status.URL, "registry_id", status.RegistryID, "kind", status.Kind, "name", status.Name, "state", status.State, "cache", status.Cache, "endpoint_queried", status.EndpointQueried, "latest_queried", status.LatestQueried, "required", status.Required)
}
