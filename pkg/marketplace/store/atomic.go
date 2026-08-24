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

	InspectionBlobs []BlobUpload
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

	for _, b := range append(req.ExtraBlobs, req.InspectionBlobs...) {
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
			if err := validateCollectionManifestGraph(rev, cr, byDigest, req.Resources, collections); err != nil {
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

func validateCollectionManifestGraph(rev ResourceRevision, cr CollectionRevision, byDigest map[ArchiveDigest][]byte, resources []ResourceRevision, collections map[string]CollectionRevision) error {
	// Decode every metadata-backed manifest first. The artifact helper then
	// validates the complete nested graph and compares every flattened Leaves
	// entry with the graph-derived result before metadata can be committed.
	manifests := make(map[string]hccartifact.CollectionManifest, len(collections))
	for id, collectionMeta := range collections {
		data, ok := byDigest[ArchiveDigest(collectionMeta.ManifestDigest)]
		if !ok {
			return fmt.Errorf("collection revision %q manifest is missing from upload graph", id)
		}
		manifest, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("collection revision %q manifest: %w", id, err)
		}
		if err := hccartifact.VerifyCollectionManifest(manifest, payload); err != nil {
			return fmt.Errorf("collection revision %q manifest: %w", id, err)
		}
		if manifest.ContentDigest != id || manifest.ContentDigest != string(collectionMeta.ManifestDigest) {
			return fmt.Errorf("%w: collection revision %q manifest digest disagrees with metadata", ErrConflict, id)
		}
		manifests[id] = manifest
	}
	rootManifest, ok := manifests[cr.ID]
	if !ok {
		return fmt.Errorf("collection revision %q manifest is missing from publication", cr.ID)
	}
	if rootManifest.Name != rev.Coordinate.Name {
		return fmt.Errorf("%w: collection %s manifest name disagrees with metadata", ErrConflict, rev.Coordinate)
	}
	if _, err := hccartifact.ValidateAndExpandCollectionManifestGraph(rootManifest, manifests); err != nil {
		return fmt.Errorf("collection %s manifest graph: %w", rev.Coordinate, err)
	}

	visiting := make(map[string]bool)
	leafRevisions := make(map[ResourceCoordinate]RevisionDigest)
	var walk func(ResourceRevision, CollectionRevision) error
	walk = func(collectionRev ResourceRevision, collectionMeta CollectionRevision) error {
		if visiting[collectionMeta.ID] {
			return fmt.Errorf("collection %s contains a cycle", collectionRev.Coordinate)
		}
		visiting[collectionMeta.ID] = true
		defer delete(visiting, collectionMeta.ID)
		manifest, ok := manifests[collectionMeta.ID]
		if !ok {
			return fmt.Errorf("collection %s manifest is missing from upload graph", collectionRev.Coordinate)
		}
		if manifest.Name != collectionRev.Coordinate.Name {
			return fmt.Errorf("%w: collection %s manifest name disagrees with metadata", ErrConflict, collectionRev.Coordinate)
		}

		metadataMembers := make(map[ResourceCoordinate]struct{}, len(collectionMeta.Members))
		for _, member := range collectionMeta.Members {
			if _, exists := metadataMembers[member]; exists {
				return fmt.Errorf("%w: collection %s metadata contains duplicate member %s", ErrConflict, collectionRev.Coordinate, member)
			}
			metadataMembers[member] = struct{}{}
		}
		if len(metadataMembers) != len(manifest.Members) {
			return fmt.Errorf("%w: collection %s metadata member count disagrees with manifest", ErrConflict, collectionRev.Coordinate)
		}
		for _, member := range manifest.Members {
			coordinate := ResourceCoordinate{Marketplace: collectionRev.Coordinate.Marketplace, Source: SourceID(member.SourceID), Kind: member.Kind, Name: member.Name}
			if _, ok := metadataMembers[coordinate]; !ok {
				return fmt.Errorf("%w: collection %s metadata is missing member %s", ErrConflict, collectionRev.Coordinate, coordinate)
			}
			memberRevision, found := resourceRevisionFor(resources, coordinate, RevisionDigest(member.Revision))
			if !found {
				return fmt.Errorf("collection %s member %s revision is missing from publication", collectionRev.Coordinate, coordinate)
			}
			if member.Kind == "collection" {
				nested, ok := collections[string(memberRevision.ContentDigest)]
				if !ok {
					return fmt.Errorf("collection %s nested member %s metadata is missing", collectionRev.Coordinate, coordinate)
				}
				if nested.SourceID != coordinate.Source || nested.ID != member.Revision || nested.ManifestDigest != RevisionDigest(member.Revision) {
					return fmt.Errorf("%w: collection %s nested member %s metadata identity disagrees with manifest", ErrConflict, collectionRev.Coordinate, coordinate)
				}
				if err := walk(memberRevision, nested); err != nil {
					return err
				}
			}
		}

		// A legacy flat manifest may omit Leaves, in which case its direct
		// materialized members are the leaves. Nested manifests are fail-closed
		// by ValidateAndExpandCollectionManifestGraph and cannot use this fallback.
		leaves := manifest.Leaves
		if len(leaves) == 0 {
			for _, member := range manifest.Members {
				if member.Kind != "collection" {
					leaves = append(leaves, member)
				}
			}
		}
		for _, member := range leaves {
			coordinate := ResourceCoordinate{Marketplace: collectionRev.Coordinate.Marketplace, Source: SourceID(member.SourceID), Kind: member.Kind, Name: member.Name}
			memberRevision, found := resourceRevisionFor(resources, coordinate, RevisionDigest(member.Revision))
			if !found {
				return fmt.Errorf("collection %s leaf %s revision is missing from publication", collectionRev.Coordinate, coordinate)
			}
			if previous, exists := leafRevisions[coordinate]; exists && previous != memberRevision.ContentDigest {
				return fmt.Errorf("%w: collection graph contains conflicting revisions for %s", ErrConflict, coordinate)
			}
			leafRevisions[coordinate] = memberRevision.ContentDigest
			if memberRevision.ArchiveDigest != "" {
				if _, ok := byDigest[memberRevision.ArchiveDigest]; !ok {
					return fmt.Errorf("collection %s leaf %s archive is missing from upload graph", collectionRev.Coordinate, coordinate)
				}
			}
		}
		return nil
	}
	return walk(rev, cr)
}

func resourceRevisionFor(resources []ResourceRevision, coordinate ResourceCoordinate, digest RevisionDigest) (ResourceRevision, bool) {
	for _, resource := range resources {
		if resource.Coordinate == coordinate && resource.ContentDigest == digest {
			return resource, true
		}
	}
	return ResourceRevision{}, false
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
