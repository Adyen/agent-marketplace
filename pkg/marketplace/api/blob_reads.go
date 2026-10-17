package api

import (
	"context"
	"math"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s *Server) readPayloadBlob(ctx context.Context, digest store.ArchiveDigest, limit int64) ([]byte, error) {
	if limit > 0 && limit < math.MaxInt64 {
		if bounded, ok := s.blobs.(store.BoundedBlobStore); ok {
			return bounded.GetBounded(ctx, digest, limit)
		}
	}
	// Keep legacy stores and unlimited/unprobeable ceilings compatible.
	// Callers retain their existing post-read admission checks.
	return s.blobs.Get(ctx, digest)
}
