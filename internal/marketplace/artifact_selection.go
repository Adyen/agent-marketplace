package marketplace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var ErrExactArtifactUnavailable = errors.New("artifact.exact-bytes-unavailable")

func exactArtifactFetchError(err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %w", ErrExactArtifactUnavailable, err)
	}
	return err
}

func selectArtifact(pin config.MarketplaceResourcePin, rev store.AssessedResourceRevision) (*artifact.ArtifactReference, error) {
	if pin.LockArtifactSchema != "" && pin.LockArtifactSchema != "v1" && pin.LockArtifactSchema != "v2" {
		return nil, artifact.ErrUnsupportedSchema
	}
	if pin.LockArtifactSchema == "v2" {
		if pin.LockArchiveDigest != "" {
			return nil, artifact.ErrInvalidReference
		}
		ref := artifact.ArtifactReference{SchemaVersion: "v2", ArtifactDigest: artifact.ArtifactDigest(pin.LockArtifactDigest), BlobDigest: artifact.BlobDigest(pin.LockBlobDigest), MediaType: artifact.MediaTypeV2}
		return &ref, ref.Validate()
	}
	if pin.LockArtifactDigest != "" || pin.LockBlobDigest != "" {
		return nil, artifact.ErrInvalidReference
	}
	if pin.LockArchiveDigest != "" || pin.LockArtifactSchema == "v1" {
		if err := validateSHA256Digest(pin.LockArchiveDigest); err != nil {
			return nil, artifact.ErrInvalidReference
		}
		return nil, nil
	}
	var selected *artifact.ArtifactReference
	for _, ref := range rev.Artifacts {
		if ref.SchemaVersion != "v2" {
			continue
		}
		if err := ref.Validate(); err != nil {
			return nil, err
		}
		if selected != nil {
			return nil, artifact.ErrInvalidReference
		}
		candidate := ref
		selected = &candidate
	}
	return selected, nil
}

func (c *Client) GetArtifactV2(ctx context.Context, ref artifact.ArtifactReference) ([]byte, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	raw, err := c.getRaw(ctx, c.requestURL([]string{"api", "v1", "blobs", "sha256", strings.TrimPrefix(string(ref.BlobDigest), "sha256:")}, nil))
	if err != nil {
		return nil, exactArtifactFetchError(err)
	}
	if _, err := artifact.VerifyV2(raw, ref, nil); err != nil {
		return nil, err
	}
	return raw, nil
}

func (r *Resolver) resolveV2Artifact(ctx context.Context, client *Client, cache *Cache, pin config.MarketplaceResourcePin, selected artifact.ArtifactReference, result resolver.ResolvedArtifact, mode resolver.MarketplaceResolveMode) (resolver.ResolvedArtifact, artifact.CanonicalArtifact, error) {
	decoded, raw, err := cache.ReadArtifactV2(selected)
	if err != nil {
		return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, err
	}
	if raw == nil {
		if mode == resolver.MarketplaceResolveModeCached || mode == resolver.MarketplaceResolveModeCachedNoFetch || client == nil {
			return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, ErrExactArtifactUnavailable
		}
		raw, err = client.GetArtifactV2(ctx, selected)
		if err != nil {
			return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, fmt.Errorf("fetch selected v2 artifact: %w", err)
		}
		decoded, err = artifact.VerifyV2(raw, selected, nil)
		if err != nil {
			return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, err
		}
	}
	if err := decoded.VerifyIdentity(pin.Kind, pin.Name, pin.Digest); err != nil {
		return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, err
	}
	resource, err := artifact.DecodeResource(pin.Kind, decoded.Payload)
	if err != nil {
		return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, err
	}
	if err := validateDecodedArtifactIdentity(pin, resource); err != nil {
		return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, err
	}
	if err := validateMirrorSourcePath(resource); err != nil {
		return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, err
	}
	if mode != resolver.MarketplaceResolveModeCached && mode != resolver.MarketplaceResolveModeCachedNoFetch {
		if err := cache.WriteArtifactV2(selected, raw); err != nil {
			return resolver.ResolvedArtifact{}, artifact.CanonicalArtifact{}, err
		}
	}
	result.Resource = resource
	result.Description = descriptionFromResource(resource)
	result.Path = cache.ArtifactPath(string(selected.BlobDigest))
	m := decoded.Manifest
	canonical := artifact.CanonicalArtifact{Manifest: artifact.CanonicalManifest{SchemaVersion: m.SchemaVersion, ArtifactVersion: m.ArtifactVersion, Kind: m.Kind, Name: m.Name, ContentDigest: m.ContentDigest, License: m.License, Capabilities: m.Capabilities}, Payload: decoded.Payload, Assets: decoded.Assets, Licenses: decoded.Licenses}
	return result, canonical, nil
}
