package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// BlobUpload is a digest-addressed blob that must be present in the blob store
// before a metadata transaction makes a revision visible.
type BlobUpload struct {
	Digest ArchiveDigest
	Data   []byte
}

// SourceRevisionPublication groups the metadata and blobs for a source revision.
// All blobs are uploaded before any metadata is written, so a failed publish
// can only leave orphaned blobs that are safe to garbage-collect.
type SourceRevisionPublication struct {
	// Occurrence is the source commit occurrence to publish.
	Occurrence SourceOccurrence

	// Resources are the resource revisions to publish.
	Resources []ResourceRevision

	// Collection is an optional collection revision to publish alongside the occurrence.
	Collection *CollectionRevision

	// ManifestPayload is the canonical source revision manifest JSON. When set,
	// it is uploaded to the blob store using Occurrence.SourceRevisionManifestDigest
	// as the key before metadata is committed.
	ManifestPayload []byte

	// ExtraBlobs contains any additional blobs that must be uploaded first, such
	// as resource and collection archives.
	ExtraBlobs []BlobUpload
}

// PublishSourceRevision uploads all required blobs, then writes the metadata in
// a single transaction. It returns an error if any blob upload fails, without
// writing metadata.
func PublishSourceRevision(ctx context.Context, meta MetadataStore, blobs BlobStore, req SourceRevisionPublication) error {
	uploads, err := prepareUploads(req)
	if err != nil {
		return err
	}

	for _, u := range uploads {
		if err := blobs.Put(ctx, u.Digest, u.Data); err != nil {
			return fmt.Errorf("upload blob %s: %w", u.Digest, err)
		}
	}

	return meta.WithTx(ctx, func(txCtx context.Context, tx MetadataStore) error {
		for _, rev := range req.Resources {
			if err := tx.PutResourceRevision(txCtx, rev); err != nil {
				return err
			}
		}
		if req.Collection != nil {
			if err := tx.PutCollectionRevision(txCtx, *req.Collection); err != nil {
				return err
			}
		}
		return tx.PutSourceOccurrence(txCtx, req.Occurrence)
	})
}

// ResourceRevisionPublication groups a single resource revision with its archive blob.
type ResourceRevisionPublication struct {
	Revision     ResourceRevision
	ArchiveBytes []byte
}

// PublishResourceRevision uploads the resource archive and writes the resource
// revision in a single transaction.
func PublishResourceRevision(ctx context.Context, meta MetadataStore, blobs BlobStore, pub ResourceRevisionPublication) error {
	archive := string(pub.Revision.ArchiveDigest)
	if archive != "" && len(pub.ArchiveBytes) > 0 {
		if err := blobs.Put(ctx, pub.Revision.ArchiveDigest, pub.ArchiveBytes); err != nil {
			return fmt.Errorf("upload resource archive %s: %w", pub.Revision.ArchiveDigest, err)
		}
	}

	return meta.WithTx(ctx, func(txCtx context.Context, tx MetadataStore) error {
		return tx.PutResourceRevision(txCtx, pub.Revision)
	})
}

func prepareUploads(req SourceRevisionPublication) ([]BlobUpload, error) {
	seen := make(map[ArchiveDigest]struct{})
	var uploads []BlobUpload

	if len(req.ManifestPayload) > 0 {
		if req.Occurrence.SourceRevisionManifestDigest == "" {
			return nil, errors.New("manifest payload provided but SourceRevisionManifestDigest is empty")
		}
		got := contentDigest(req.ManifestPayload)
		want := string(req.Occurrence.SourceRevisionManifestDigest)
		if got != want {
			return nil, fmt.Errorf("manifest payload digest mismatch: got %s, want %s", got, want)
		}
		uploads = appendUpload(uploads, seen, ArchiveDigest(want), req.ManifestPayload)
	}

	for _, b := range req.ExtraBlobs {
		if b.Digest == "" {
			return nil, errors.New("blob upload with empty digest")
		}
		uploads = appendUpload(uploads, seen, b.Digest, b.Data)
	}

	return uploads, nil
}

func appendUpload(uploads []BlobUpload, seen map[ArchiveDigest]struct{}, digest ArchiveDigest, data []byte) []BlobUpload {
	if digest == "" {
		return uploads
	}
	if _, ok := seen[digest]; ok {
		return uploads
	}
	seen[digest] = struct{}{}
	return append(uploads, BlobUpload{Digest: digest, Data: data})
}

func contentDigest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}
