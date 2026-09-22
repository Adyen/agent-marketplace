package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s *Server) loadArtifactReferences(w http.ResponseWriter, r *http.Request, rev *store.AssessedResourceRevision) bool {
	source, ok := s.meta.(store.ResourceArtifactStore)
	if !ok {
		return true
	}
	records, err := source.ListResourceArtifacts(r.Context(), rev.Coordinate, rev.ContentDigest)
	if err != nil {
		slog.ErrorContext(r.Context(), "artifact references lookup failed", "coordinate", rev.Coordinate, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load artifact references")
		return false
	}
	rev.Artifacts = nil
	for _, record := range records {
		rev.Artifacts = append(rev.Artifacts, record.Reference)
	}
	return true
}

func (s *Server) handleArtifactBlobs(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/blobs/"), "/")
	if len(parts) != 2 || parts[0] != "sha256" || !isLowerHexDigest(parts[1]) {
		writeError(w, http.StatusBadRequest, "invalid blob digest")
		return
	}
	digest := artifact.BlobDigest("sha256:" + parts[1])
	source, ok := s.meta.(store.ResourceArtifactStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "artifact associations unavailable")
		return
	}
	owners, err := source.ListResourceArtifactsByBlob(r.Context(), s.mpID, digest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load blob owners")
		return
	}
	if len(owners) == 0 {
		writeError(w, http.StatusNotFound, "artifact blob not found")
		return
	}
	revisions := make([]store.AssessedResourceRevision, 0, len(owners))
	for _, owner := range owners {
		rev, err := s.meta.GetResourceRevision(r.Context(), owner.Coordinate, owner.ContentDigest)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "artifact owner unavailable")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load artifact owner")
			return
		}
		revisions = append(revisions, rev)
	}
	restriction, err := s.managedOwnerRestriction(r.Context(), revisions)
	if err != nil {
		writeAvailabilityRestriction(w, restriction, err)
		return
	}
	switch restriction {
	case AvailabilityRevoked:
		writeError(w, http.StatusGone, "artifact blob is revoked")
		return
	case AvailabilityBlocked:
		writeError(w, http.StatusForbidden, "artifact blob is blocked")
		return
	}
	limit := manifestPayloadLimit(s.cfg)
	bounded, ok := s.blobs.(store.BoundedBlobStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "bounded artifact storage unavailable")
		return
	}
	raw, err := bounded.GetBounded(r.Context(), store.ArchiveDigest(digest), limit)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "artifact blob not found")
		return
	}
	if err == nil {
		for _, owner := range owners {
			var decoded artifact.ArtifactV2
			decoded, err = artifact.VerifyV2(raw, owner.Reference, nil)
			if err == nil {
				err = decoded.VerifyIdentity(owner.Coordinate.Kind, owner.Coordinate.Name, string(owner.ContentDigest))
			}
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "artifact blob integrity failure", "blob_digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "artifact blob integrity failure")
		return
	}
	writeImmutable(w, artifact.MediaTypeV2, raw)
}
