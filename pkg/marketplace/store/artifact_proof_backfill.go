package store

import (
	"context"
	"errors"
	"fmt"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

const (
	ArtifactProofBackfillMaxPage = 100
)

var ErrArtifactProofBackfillIncompatible = errors.New("marketplace/store: artifact proof backfill requires the original Go compression toolchain")

// ArtifactProofBackfillStore pages every distinct v1 archive digest, including
// historical revisions, without loading an unbounded result set.
type ArtifactProofBackfillStore interface {
	ListArtifactArchiveDigests(context.Context, MarketplaceID, ArchiveDigest, int) ([]ArchiveDigest, error)
}

type boundedArtifactProofBlobStore interface {
	BlobStore
	BoundedBlobStore
}

// ArtifactProofBackfillRequest selects one bounded page and its read budget.
type ArtifactProofBackfillRequest struct {
	Marketplace MarketplaceID
	After       ArchiveDigest
	Limit       int
	BlobLimit   int64
}

// ArtifactProofBackfillResult describes one bounded, resumable backfill page.
type ArtifactProofBackfillResult struct {
	Processed int           `json:"processed"`
	Created   int           `json:"created"`
	Existing  int           `json:"existing"`
	NextAfter ArchiveDigest `json:"next_after,omitempty"`
	Remaining bool          `json:"remaining"`
}

// BackfillArtifactProofs creates retained placeholder-form proofs for one page
// of existing v1 artifacts. It fails closed if the active compressor cannot
// reproduce the exact published archive bytes.
func BackfillArtifactProofs(ctx context.Context, meta MetadataStore, blobs BlobStore, req ArtifactProofBackfillRequest) (ArtifactProofBackfillResult, error) {
	var result ArtifactProofBackfillResult
	if req.Marketplace == "" {
		return result, fmt.Errorf("artifact proof backfill marketplace is required")
	}
	if req.Limit < 1 || req.Limit > ArtifactProofBackfillMaxPage {
		return result, fmt.Errorf("artifact proof backfill limit must be between 1 and %d", ArtifactProofBackfillMaxPage)
	}
	if req.After != "" && !validInspectionDigest(string(req.After)) {
		return result, fmt.Errorf("artifact proof backfill cursor is invalid")
	}
	if req.BlobLimit < 1 {
		return result, fmt.Errorf("artifact proof backfill blob limit must be positive")
	}
	pager, ok := meta.(ArtifactProofBackfillStore)
	if !ok {
		return result, ErrTopology
	}
	bounded, ok := blobs.(boundedArtifactProofBlobStore)
	if !ok {
		return result, ErrTopology
	}
	digests, err := pager.ListArtifactArchiveDigests(ctx, req.Marketplace, req.After, req.Limit+1)
	if err != nil {
		return result, fmt.Errorf("list artifact proof backfill page: %w", err)
	}
	if len(digests) > req.Limit {
		result.Remaining = true
		digests = digests[:req.Limit]
	}
	for _, digest := range digests {
		if !validInspectionDigest(string(digest)) {
			return result, fmt.Errorf("artifact proof backfill encountered invalid digest %q", digest)
		}
		existed, err := backfillArtifactProof(ctx, bounded, digest, req.BlobLimit)
		if err != nil {
			return result, err
		}
		result.Processed++
		result.NextAfter = digest
		if existed {
			result.Existing++
		} else {
			result.Created++
		}
	}
	return result, nil
}

func backfillArtifactProof(ctx context.Context, blobs boundedArtifactProofBlobStore, digest ArchiveDigest, blobLimit int64) (bool, error) {
	proofKey := ArtifactProofBlobKey(digest)
	proof, err := blobs.GetBounded(ctx, proofKey, blobLimit)
	if err == nil {
		if _, decodeErr := hccartifact.DecodeProof(proof, string(digest), nil); decodeErr != nil {
			return false, fmt.Errorf("validate existing artifact proof %s: %w", digest, decodeErr)
		}
		return true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return false, fmt.Errorf("read existing artifact proof %s: %w", digest, err)
	}

	raw, err := blobs.GetBounded(ctx, digest, blobLimit)
	if err != nil {
		return false, fmt.Errorf("read artifact %s for proof backfill: %w", digest, err)
	}
	proof, err = hccartifact.ReproduceProof(raw, string(digest), nil)
	if err != nil {
		if errors.Is(err, hccartifact.ErrArchiveDigestMismatch) || errors.Is(err, hccartifact.ErrArchiveBytesMismatch) {
			return false, fmt.Errorf("%w: artifact %s", ErrArtifactProofBackfillIncompatible, digest)
		}
		return false, fmt.Errorf("reproduce artifact %s proof: %w", digest, err)
	}
	if err := putImmutableBlobBounded(ctx, blobs, BlobUpload{Digest: proofKey, Data: proof}, blobLimit); err != nil {
		return false, fmt.Errorf("write artifact proof %s: %w", digest, err)
	}
	return false, nil
}
