package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func legacyArtifactReference(rev ResourceRevision) artifact.LegacyReference {
	return artifact.LegacyReference{ArchiveDigest: string(rev.ArchiveDigest), Kind: rev.Coordinate.Kind, Name: rev.Coordinate.Name, ContentDigest: string(rev.ContentDigest)}
}

func PrepareArtifactV2Publication(ctx context.Context, meta MetadataStore, blobs BlobStore, req RegistryRevisionPublication) (RegistryRevisionPublication, error) {
	if req.Occurrence.Acquisition != AcquisitionMirror {
		return req, nil
	}
	associations, ok := meta.(ResourceArtifactStore)
	if !ok {
		return req, fmt.Errorf("%w: artifact associations unsupported", ErrConflict)
	}
	req.ExtraBlobs = slices.Clone(req.ExtraBlobs)
	req.Artifacts = slices.Clone(req.Artifacts)
	byDigest := make(map[ArchiveDigest][]byte, len(req.ExtraBlobs))
	for _, b := range req.ExtraBlobs {
		byDigest[b.Digest] = b.Data
	}
	for _, rev := range req.Resources {
		if rev.Coordinate.Kind == "collection" || rev.ArchiveDigest == "" {
			continue
		}
		raw, ok := byDigest[rev.ArchiveDigest]
		if !ok {
			return req, fmt.Errorf("%w: v1 publication bytes missing", ErrBlobMissing)
		}
		candidate, expected, err := artifact.ConvertV1ToV2(raw, legacyArtifactReference(rev), nil)
		if errors.Is(err, artifact.ErrUnsupportedConversion) {
			slog.WarnContext(ctx, "artifact v2 conversion ineligible", "code", "artifact.conversion-unsupported", "coordinate", rev.Coordinate)
			continue
		}
		if err != nil {
			return req, err
		}
		prior, err := meta.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return req, err
		}
		if err == nil {
			if prior.ArchiveDigest != rev.ArchiveDigest {
				return req, fmt.Errorf("%w: legacy artifact changed", ErrConflict)
			}
			source, err := readArtifactBlob(ctx, blobs, prior.ArchiveDigest)
			if errors.Is(err, ErrNotFound) {
				slog.WarnContext(ctx, "artifact v2 source unavailable", "code", "artifact.exact-bytes-unavailable", "coordinate", rev.Coordinate)
				continue
			}
			if err != nil {
				return req, err
			}
			converted, sourceDigest, err := artifact.ConvertV1ToV2(source, legacyArtifactReference(prior.ResourceRevision), nil)
			if errors.Is(err, artifact.ErrUnsupportedConversion) {
				slog.WarnContext(ctx, "artifact v2 source ineligible", "code", "artifact.conversion-unsupported", "coordinate", rev.Coordinate)
				continue
			}
			if err != nil {
				return req, err
			}
			if sourceDigest != expected {
				return req, artifact.ErrProtectedEntryMismatch
			}
			candidate = converted
		}
		existing, err := associations.ListResourceArtifacts(ctx, rev.Coordinate, rev.ContentDigest)
		if err != nil {
			return req, err
		}
		var encoded artifact.EncodedArtifact
		if len(existing) > 0 {
			selected := existing[0]
			if selected.Reference.ArtifactDigest != expected {
				return req, artifact.ErrProtectedEntryMismatch
			}
			body, err := readArtifactBlob(ctx, blobs, ArchiveDigest(selected.Reference.BlobDigest))
			if err != nil {
				return req, err
			}
			if err := artifact.VerifyV1V2Equivalence(raw, legacyArtifactReference(rev), body, selected.Reference, nil); err != nil {
				return req, err
			}
			encoded = artifact.EncodedArtifact{Bytes: body, Reference: selected.Reference}
		} else {
			encoded, err = artifact.EncodeV2(candidate, nil)
			if err != nil {
				return req, err
			}
		}
		req.Artifacts = append(req.Artifacts, ResourceArtifact{Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Reference: encoded.Reference})
		req.ExtraBlobs = append(req.ExtraBlobs, BlobUpload{Digest: ArchiveDigest(encoded.Reference.BlobDigest), Data: encoded.Bytes})
	}
	slog.InfoContext(ctx, "prepared artifact v2 publication", "artifacts", len(req.Artifacts))
	return req, nil
}

func validateArtifactPublication(req RegistryRevisionPublication, uploads []BlobUpload) error {
	byDigest := make(map[ArchiveDigest][]byte, len(uploads))
	for _, b := range uploads {
		byDigest[b.Digest] = b.Data
	}
	seen := make(map[ResourceCoordinate]bool, len(req.Artifacts))
	for _, a := range req.Artifacts {
		if err := a.Validate(); err != nil {
			return err
		}
		rev, ok := resourceRevisionFor(req.Resources, a.Coordinate, a.ContentDigest)
		if !ok || rev.ArchiveDigest == "" || seen[a.Coordinate] {
			return artifact.ErrInvalidReference
		}
		seen[a.Coordinate] = true
		raw, ok := byDigest[ArchiveDigest(a.Reference.BlobDigest)]
		if !ok {
			return ErrBlobMissing
		}
		legacy, ok := byDigest[rev.ArchiveDigest]
		if !ok {
			return ErrBlobMissing
		}
		if err := artifact.VerifyV1V2Equivalence(legacy, legacyArtifactReference(rev), raw, a.Reference, nil); err != nil {
			return fmt.Errorf("validate paired artifacts: %w", err)
		}
	}
	return nil
}

func BackfillResourceArtifact(ctx context.Context, meta MetadataStore, blobs BlobStore, c ResourceCoordinate, d RevisionDigest) (ResourceArtifact, error) {
	rev, err := meta.GetResourceRevision(ctx, c, d)
	if err != nil {
		return ResourceArtifact{}, err
	}
	raw, err := readArtifactBlob(ctx, blobs, rev.ArchiveDigest)
	if err != nil {
		return ResourceArtifact{}, err
	}
	req, err := PrepareArtifactV2Publication(ctx, meta, blobs, RegistryRevisionPublication{Occurrence: RegistryOccurrence{Acquisition: AcquisitionMirror}, Resources: []ResourceRevision{rev.ResourceRevision}, ExtraBlobs: []BlobUpload{{Digest: rev.ArchiveDigest, Data: raw}}})
	if err != nil {
		return ResourceArtifact{}, err
	}
	if len(req.Artifacts) != 1 {
		return ResourceArtifact{}, artifact.ErrUnsupportedConversion
	}
	a := req.Artifacts[0]
	upload := req.ExtraBlobs[len(req.ExtraBlobs)-1]
	if err := putImmutableBlob(ctx, blobs, upload); err != nil {
		return ResourceArtifact{}, err
	}
	err = meta.WithTx(ctx, func(ctx context.Context, tx MetadataStore) error {
		return putResourceArtifacts(ctx, tx, []ResourceArtifact{a})
	})
	return a, err
}
