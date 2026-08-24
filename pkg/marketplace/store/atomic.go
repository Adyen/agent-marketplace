package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
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

	// Collection is retained for compatibility with callers publishing one
	// collection revision.
	Collection *CollectionRevision

	// Collections contains all collection revisions discovered in the source
	// commit. They are committed with the source occurrence as one publication.
	Collections []CollectionRevision

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
	if err := validateSourceRevisionPublication(req); err != nil {
		return err
	}
	uploads, err := prepareUploads(req)
	if err != nil {
		return err
	}
	if err := validateCollectionPublication(req, uploads); err != nil {
		return err
	}

	immutableDigests := collectionManifestDigests(req)
	for _, u := range uploads {
		if _, immutable := immutableDigests[u.Digest]; immutable {
			if err := putImmutableBlob(ctx, blobs, u); err != nil {
				return fmt.Errorf("upload immutable collection blob %s: %w", u.Digest, err)
			}
			continue
		}
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
		for _, collection := range req.Collections {
			if err := tx.PutCollectionRevision(txCtx, collection); err != nil {
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

func validateSourceRevisionPublication(req SourceRevisionPublication) error {
	for _, rev := range req.Resources {
		if rev.Coordinate.Kind == "collection" && rev.ArchiveDigest != "" {
			return fmt.Errorf("collection resource revision %s must not have an archive digest", rev.Coordinate)
		}
	}
	return nil
}

func prepareUploads(req SourceRevisionPublication) ([]BlobUpload, error) {
	seen := make(map[ArchiveDigest]int)
	var uploads []BlobUpload

	collectionDigests := collectionManifestDigests(req)
	archiveDigests := make(map[ArchiveDigest]struct{}, len(req.Resources))
	for _, rev := range req.Resources {
		if rev.ArchiveDigest != "" {
			archiveDigests[rev.ArchiveDigest] = struct{}{}
		}
	}
	add := func(b BlobUpload) error {
		if b.Digest == "" {
			return errors.New("blob upload with empty digest")
		}
		// Collection manifest addresses use the manifest's content_digest,
		// which intentionally hashes the canonical payload with that field
		// cleared rather than the complete self-referential blob bytes.
		if _, isCollectionManifest := collectionDigests[b.Digest]; !isCollectionManifest {
			if got := contentDigest(b.Data); got != string(b.Digest) {
				// Canonical artifact archives use a placeholder digest to break
				// their self-reference, so their address is verified by the
				// artifact verifier rather than by hashing the final bytes.
				if _, isArchive := archiveDigests[b.Digest]; !isArchive || !verifyArchiveBlob(b) {
					return fmt.Errorf("blob %s data digest mismatch: got %s", b.Digest, got)
				}
			}
		}
		if i, ok := seen[b.Digest]; ok {
			if !bytes.Equal(uploads[i].Data, b.Data) {
				return fmt.Errorf("%w: blob %s supplied with different data", ErrConflict, b.Digest)
			}
			return nil
		}
		seen[b.Digest] = len(uploads)
		uploads = append(uploads, BlobUpload{Digest: b.Digest, Data: append([]byte(nil), b.Data...)})
		return nil
	}

	if len(req.ManifestPayload) > 0 {
		if req.Occurrence.SourceRevisionManifestDigest == "" {
			return nil, errors.New("manifest payload provided but SourceRevisionManifestDigest is empty")
		}
		got := contentDigest(req.ManifestPayload)
		want := string(req.Occurrence.SourceRevisionManifestDigest)
		if got != want {
			return nil, fmt.Errorf("manifest payload digest mismatch: got %s, want %s", got, want)
		}
		if err := add(BlobUpload{Digest: ArchiveDigest(want), Data: req.ManifestPayload}); err != nil {
			return nil, err
		}
	}

	for _, b := range req.ExtraBlobs {
		if err := add(b); err != nil {
			return nil, err
		}
	}

	return uploads, nil
}

// validateCollectionPublication validates the complete flat collection upload
// graph before any metadata transaction begins. A collection's manifest and
// each exact leaf archive must be present in the same publication request.
func validateCollectionPublication(req SourceRevisionPublication, uploads []BlobUpload) error {
	byDigest := make(map[ArchiveDigest][]byte, len(uploads))
	for _, upload := range uploads {
		byDigest[upload.Digest] = upload.Data
	}

	collections := make(map[string]CollectionRevision, len(req.Collections)+1)
	addCollection := func(cr CollectionRevision) error {
		if cr.ID == "" {
			return errors.New("collection revision has empty ID")
		}
		if previous, ok := collections[cr.ID]; ok {
			if !previous.Equal(cr) {
				return fmt.Errorf("%w: collection revision %q supplied with different metadata", ErrConflict, cr.ID)
			}
			return nil
		}
		collections[cr.ID] = cr
		return nil
	}
	if req.Collection != nil {
		if err := addCollection(*req.Collection); err != nil {
			return err
		}
	}
	for _, cr := range req.Collections {
		if err := addCollection(cr); err != nil {
			return err
		}
	}

	for _, rev := range req.Resources {
		if rev.Coordinate.Kind == "collection" {
			cr, ok := collections[string(rev.ContentDigest)]
			if !ok {
				return fmt.Errorf("collection resource revision %s has no collection metadata", rev.Coordinate)
			}
			if cr.ID != string(rev.ContentDigest) || cr.ManifestDigest != rev.ContentDigest || cr.SourceID != rev.Coordinate.Source {
				return fmt.Errorf("%w: collection %s metadata does not agree with resource revision", ErrConflict, rev.Coordinate)
			}
			if err := validateCollectionManifestGraph(rev, cr, byDigest, req.Resources); err != nil {
				return err
			}
		}
	}

	for id, cr := range collections {
		found := false
		for _, rev := range req.Resources {
			if rev.Coordinate.Kind == "collection" && string(rev.ContentDigest) == id {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("collection revision %q has no collection resource revision", id)
		}
		if _, ok := byDigest[ArchiveDigest(cr.ManifestDigest)]; !ok {
			return fmt.Errorf("collection revision %q manifest is missing from upload graph", id)
		}
	}
	return nil
}

func validateCollectionManifestGraph(rev ResourceRevision, cr CollectionRevision, byDigest map[ArchiveDigest][]byte, resources []ResourceRevision) error {
	data, ok := byDigest[ArchiveDigest(cr.ManifestDigest)]
	if !ok {
		return fmt.Errorf("collection %s manifest is missing from upload graph", rev.Coordinate)
	}
	manifest, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("collection %s manifest: %w", rev.Coordinate, err)
	}
	if err := hccartifact.VerifyCollectionManifest(manifest, payload); err != nil {
		return fmt.Errorf("collection %s manifest: %w", rev.Coordinate, err)
	}
	if err := hccartifact.ValidateFlatMirrorManifest(manifest); err != nil {
		return fmt.Errorf("collection %s manifest: %w", rev.Coordinate, err)
	}
	if manifest.ContentDigest != string(cr.ManifestDigest) || manifest.Name != rev.Coordinate.Name {
		return fmt.Errorf("%w: collection %s manifest digest or name disagrees with metadata", ErrConflict, rev.Coordinate)
	}

	metadataMembers := make(map[ResourceCoordinate]struct{}, len(cr.Members))
	for _, member := range cr.Members {
		if _, exists := metadataMembers[member]; exists {
			return fmt.Errorf("%w: collection %s metadata contains duplicate member %s", ErrConflict, rev.Coordinate, member)
		}
		metadataMembers[member] = struct{}{}
	}
	if len(metadataMembers) != len(manifest.Members) {
		return fmt.Errorf("%w: collection %s metadata member count disagrees with manifest", ErrConflict, rev.Coordinate)
	}
	for _, member := range manifest.Members {
		coordinate := ResourceCoordinate{Marketplace: rev.Coordinate.Marketplace, Source: SourceID(member.SourceID), Kind: member.Kind, Name: member.Name}
		if _, ok := metadataMembers[coordinate]; !ok {
			return fmt.Errorf("%w: collection %s metadata is missing member %s", ErrConflict, rev.Coordinate, coordinate)
		}
		var leaf *ResourceRevision
		for i := range resources {
			if resources[i].Coordinate == coordinate && resources[i].ContentDigest == RevisionDigest(member.Revision) {
				leaf = &resources[i]
				break
			}
		}
		if leaf == nil {
			return fmt.Errorf("collection %s leaf %s revision is missing from publication", rev.Coordinate, coordinate)
		}
		if leaf.ArchiveDigest == "" {
			return fmt.Errorf("collection %s leaf %s has no mirrored archive", rev.Coordinate, coordinate)
		}
		if _, ok := byDigest[leaf.ArchiveDigest]; !ok {
			return fmt.Errorf("collection %s leaf %s archive is missing from upload graph", rev.Coordinate, coordinate)
		}
	}
	return nil
}

func collectionManifestDigests(req SourceRevisionPublication) map[ArchiveDigest]struct{} {
	out := make(map[ArchiveDigest]struct{}, len(req.Collections)+1)
	if req.Collection != nil && req.Collection.ManifestDigest != "" {
		out[ArchiveDigest(req.Collection.ManifestDigest)] = struct{}{}
	}
	for _, cr := range req.Collections {
		if cr.ManifestDigest != "" {
			out[ArchiveDigest(cr.ManifestDigest)] = struct{}{}
		}
	}
	return out
}

// immutableBlobStore is implemented by adapters that can atomically preserve a
// first writer. The read-before-write fallback still protects adapters without
// this optional capability from replacing an existing differing collection
// blob.
type immutableBlobStore interface {
	PutImmutable(context.Context, ArchiveDigest, []byte) error
}

func putImmutableBlob(ctx context.Context, blobs BlobStore, upload BlobUpload) error {
	if existing, err := blobs.Get(ctx, upload.Digest); err == nil {
		if !bytes.Equal(existing, upload.Data) {
			return fmt.Errorf("%w: collection blob %s already contains different data", ErrConflict, upload.Digest)
		}
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if immutable, ok := blobs.(immutableBlobStore); ok {
		return immutable.PutImmutable(ctx, upload.Digest, upload.Data)
	}
	return blobs.Put(ctx, upload.Digest, upload.Data)
}

func contentDigest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

func verifyArchiveBlob(upload BlobUpload) bool {
	canonical, raw, err := hccartifact.Decode(bytes.NewReader(upload.Data))
	if err != nil {
		return false
	}
	if err := canonical.VerifyArchiveDigest(raw); err != nil {
		return false
	}
	return canonical.Manifest.ArchiveDigest == string(upload.Digest)
}
