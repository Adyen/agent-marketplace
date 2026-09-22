package marketplace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// ReconcileArtifactLocks upgrades legacy artifact lock entries to v2 selections.
// An optional per-invocation revocation policy keeps explicit --allow-revoked
// overrides honored while reconciling a writable lock.
func (r *Resolver) ReconcileArtifactLocks(ctx context.Context, cfg *config.Config, input lockfile.Document, policies ...resolver.MarketplaceRevocationPolicy) (lockfile.Document, bool, error) {
	var policy resolver.MarketplaceRevocationPolicy
	if len(policies) > 0 {
		policy = policies[0]
	}
	doc, err := lockfile.Normalize(input)
	if err != nil {
		return input, false, err
	}
	changed := false
	for i := range doc.Marketplaces {
		mp := &doc.Marketplaces[i]
		entry := config.MarketplaceEntry{URL: mp.URL, InstanceID: mp.InstanceID}
		configured := false
		if cfg != nil {
			for _, candidate := range cfg.Marketplaces {
				if candidate.URL == mp.URL && candidate.InstanceID == mp.InstanceID {
					configured = true
					break
				}
			}
		}
		if !configured {
			continue
		}
		var allowed []string
		if cfg.AllowedMarketplaces != nil {
			allowed = *cfg.AllowedMarketplaces
		}
		if err := config.ValidateMarketplaceURLs([]config.MarketplaceEntry{entry}, allowed); err != nil {
			return input, false, err
		}
		var client *Client
		for j := range mp.Resources {
			locked := &mp.Resources[j]
			if locked.Acquisition != lockfile.MarketplaceAcquisitionMirror || locked.Kind == "collection" || locked.Artifact != nil && locked.Artifact.Schema == "v2" {
				continue
			}
			if client == nil {
				client, err = r.clientForEntry(entry)
				if err != nil {
					return input, false, err
				}
				wk, err := client.WellKnown(ctx)
				if err != nil {
					return input, false, err
				}
				if wk.InstanceID != entry.InstanceID {
					return input, false, ErrArtifactIdentityMismatch
				}
			}
			legacyDigest := string(locked.ArchiveDigest)
			if locked.Artifact != nil {
				legacyDigest = string(locked.Artifact.ArchiveDigest)
			}
			pin := config.MarketplaceResourcePin{RegistryID: locked.RegistryID, RegistrySlug: locked.RegistryName, Kind: locked.Kind, Name: locked.Name, Revision: locked.Revision, Digest: string(locked.ContentDigest), Acquisition: config.MarketplaceAcquisitionMirror, LockArchiveDigest: legacyDigest}
			rev, err := client.GetResourceRevision(ctx, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
			if err != nil {
				return input, false, err
			}
			if err := validateResourceRevision(entry, pin, rev); err != nil {
				return input, false, err
			}
			if err := enforceMarketplaceUse(rev, policy); err != nil {
				return input, false, err
			}
			unlocked := pin
			unlocked.LockArchiveDigest = ""
			selected, err := selectArtifact(unlocked, rev)
			if err != nil {
				return input, false, err
			}
			if selected == nil {
				continue
			}
			source, _, err := r.cache().ReadArtifact(legacyDigest)
			if err != nil {
				return input, false, err
			}
			if source == nil {
				source, err = client.GetArtifact(ctx, legacyDigest)
				var apiErr *APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
					slog.WarnContext(ctx, "artifact lock upgrade source unavailable", "code", "artifact.exact-bytes-unavailable", "registry", pin.RegistryID, "kind", pin.Kind, "name", pin.Name)
					continue
				}
				if err != nil {
					return input, false, err
				}
			}
			legacy := artifact.LegacyReference{ArchiveDigest: legacyDigest, Kind: pin.Kind, Name: pin.Name, ContentDigest: pin.Digest}
			_, _, err = artifact.ConvertV1ToV2(source, legacy, nil)
			if errors.Is(err, artifact.ErrUnsupportedConversion) {
				slog.WarnContext(ctx, "artifact lock upgrade ineligible", "code", "artifact.conversion-unsupported", "registry", pin.RegistryID, "kind", pin.Kind, "name", pin.Name)
				continue
			}
			if err != nil {
				return input, false, err
			}
			candidate, err := client.GetArtifactV2(ctx, *selected)
			if err != nil {
				return input, false, fmt.Errorf("verify upgrade candidate: %w", err)
			}
			if err := artifact.VerifyV1V2Equivalence(source, legacy, candidate, *selected, nil); err != nil {
				return input, false, err
			}
			if err := r.cache().WriteArtifactV2(*selected, candidate); err != nil {
				return input, false, err
			}
			if _, err := r.cache().WriteResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, rev); err != nil {
				return input, false, err
			}
			locked.Artifact = &lockfile.MarketplaceArtifactLock{Schema: "v2", ArtifactDigest: lockfile.Digest(selected.ArtifactDigest), BlobDigest: lockfile.Digest(selected.BlobDigest)}
			locked.ArchiveDigest = ""
			changed = true
		}
	}
	if !changed {
		return input, false, nil
	}
	normalized, err := lockfile.NormalizeArtifactSelections(doc)
	if err != nil {
		return input, false, err
	}
	return normalized, true, nil
}
