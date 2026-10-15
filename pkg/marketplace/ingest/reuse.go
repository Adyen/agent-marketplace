package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// storedArchive is an archive already published for a resource revision.
type storedArchive struct {
	digest      store.ArchiveDigest
	final       []byte
	placeholder []byte // nil when the stored revision has no placeholder archive
}

// ReuseStoredArchives keeps the archive already stored for a mirror resource when
// the newly built artifact of the same content is equal to it ignoring archive
// digest. Archives are compared without re-encoding: a stored placeholder form is
// verified by hash and a stored final archive by content. The registry revision
// manifest and occurrence digests are then rebuilt to carry the reused digests,
// so republishing identical content does not churn digests across toolchains.
func ReuseStoredArchives(ctx context.Context, meta store.MetadataStore, blobs store.BlobStore, rev *Revision) error {
	substitutions := make(map[string]string)
	for i := range rev.Resources {
		res := &rev.Resources[i]
		if res.ArchiveDigest == "" {
			continue
		}
		newFinal, ok := blobData(rev.ExtraBlobs, res.ArchiveDigest)
		if !ok {
			continue
		}
		stored, ok, err := storedIdenticalArchive(ctx, meta, blobs, res.Coordinate, res.ContentDigest, newFinal)
		if err != nil {
			return err
		}
		if !ok || stored.digest == res.ArchiveDigest {
			continue
		}
		substitutions[string(res.ArchiveDigest)] = string(stored.digest)
		rev.ExtraBlobs = replaceArchiveBlobs(rev.ExtraBlobs, res.ArchiveDigest, stored)
		res.ArchiveDigest = stored.digest
	}
	if len(substitutions) == 0 {
		return nil
	}
	return rebuildRegistryRevisionManifest(rev, substitutions)
}

// storedIdenticalArchive returns the archive stored for the revision of coord with
// contentDigest when that stored artifact is equal to the newly built final archive.
func storedIdenticalArchive(ctx context.Context, meta store.MetadataStore, blobs store.BlobStore, coord store.ResourceCoordinate, contentDigest store.RevisionDigest, newFinal []byte) (storedArchive, bool, error) {
	existing, err := meta.GetResourceRevision(ctx, coord, contentDigest)
	if errors.Is(err, store.ErrNotFound) {
		return storedArchive{}, false, nil
	}
	if err != nil {
		return storedArchive{}, false, fmt.Errorf("load stored revision %v: %w", coord, err)
	}
	if existing.ArchiveDigest == "" {
		return storedArchive{}, false, nil
	}
	final, err := blobs.Get(ctx, existing.ArchiveDigest)
	if errors.Is(err, store.ErrNotFound) {
		return storedArchive{}, false, nil
	}
	if err != nil {
		return storedArchive{}, false, fmt.Errorf("load stored archive %s: %w", existing.ArchiveDigest, err)
	}
	stored := storedArchive{digest: existing.ArchiveDigest, final: final}

	var storedArt hccartifact.CanonicalArtifact
	placeholder, err := blobs.Get(ctx, store.PlaceholderArchiveKey(existing.ArchiveDigest))
	switch {
	case err == nil:
		stored.placeholder = placeholder
		storedArt, err = hccartifact.VerifyPlaceholderArchive(placeholder, string(existing.ArchiveDigest))
		if err != nil {
			return storedArchive{}, false, nil
		}
		if _, err := hccartifact.VerifyFinalAgainstPlaceholder(final, storedArt); err != nil {
			return storedArchive{}, false, nil
		}
	case errors.Is(err, store.ErrNotFound):
		storedArt, _, err = hccartifact.Decode(bytes.NewReader(final))
		if err != nil {
			return storedArchive{}, false, nil
		}
	default:
		return storedArchive{}, false, fmt.Errorf("load stored placeholder archive %s: %w", existing.ArchiveDigest, err)
	}

	newArt, _, err := hccartifact.Decode(bytes.NewReader(newFinal))
	if err != nil {
		return storedArchive{}, false, fmt.Errorf("decode built archive for %v: %w", coord, err)
	}
	if !hccartifact.EqualIgnoringArchiveDigest(storedArt, newArt) {
		return storedArchive{}, false, nil
	}
	return stored, true, nil
}

// rebuildRegistryRevisionManifest applies archive digest substitutions to the
// registry revision manifest and propagates its new digest to the occurrences.
func rebuildRegistryRevisionManifest(rev *Revision, substitutions map[string]string) error {
	manifest, _, err := artifact.DecodeRegistryRevisionManifest(bytes.NewReader(rev.ManifestPayload))
	if err != nil {
		return fmt.Errorf("decode registry revision manifest: %w", err)
	}
	_, digest, err := artifact.CanonicalRegistryRevisionPayload(manifest)
	if err != nil {
		return fmt.Errorf("canonical registry revision manifest: %w", err)
	}
	if store.RevisionDigest(digest) != rev.ManifestDigest {
		return fmt.Errorf("registry revision manifest digest %s does not round-trip to %s", rev.ManifestDigest, digest)
	}
	for i := range manifest.Resources {
		if archive, ok := substitutions[manifest.Resources[i].ArchiveDigest]; ok {
			manifest.Resources[i].ArchiveDigest = archive
		}
	}
	payload, digest, err := artifact.CanonicalRegistryRevisionPayload(manifest)
	if err != nil {
		return fmt.Errorf("canonical registry revision manifest: %w", err)
	}
	rev.ManifestPayload = payload
	rev.ManifestDigest = store.RevisionDigest(digest)
	rev.Occurrence.RegistryRevisionManifestDigest = rev.ManifestDigest
	for i := range rev.ResourceOccurrences {
		rev.ResourceOccurrences[i].ManifestDigest = rev.ManifestDigest
	}
	return nil
}

// replaceArchiveBlobs drops the newly built archive and its placeholder form and
// uploads the stored archive in their place. A stored revision with no
// placeholder archive leaves the placeholder blob out, because the built one
// does not hash to the stored digest.
func replaceArchiveBlobs(blobs []store.BlobUpload, newDigest store.ArchiveDigest, stored storedArchive) []store.BlobUpload {
	newPlaceholder := store.PlaceholderArchiveKey(newDigest)
	out := make([]store.BlobUpload, 0, len(blobs)+2)
	for _, b := range blobs {
		if b.Digest != newDigest && b.Digest != newPlaceholder {
			out = append(out, b)
		}
	}
	out = appendUpload(out, stored.digest, stored.final)
	if stored.placeholder != nil {
		out = appendUpload(out, store.PlaceholderArchiveKey(stored.digest), stored.placeholder)
	}
	return out
}

func blobData(blobs []store.BlobUpload, digest store.ArchiveDigest) ([]byte, bool) {
	for _, b := range blobs {
		if b.Digest == digest {
			return b.Data, true
		}
	}
	return nil, false
}
