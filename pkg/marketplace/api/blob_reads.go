package api

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var errPayloadReadLimit = errors.New("payload exceeds read limit")

func (s *Server) readPayloadBlob(ctx context.Context, digest store.ArchiveDigest, limit int64) ([]byte, error) {
	if limit > 0 && limit < math.MaxInt64 {
		if bounded, ok := s.blobs.(store.BoundedBlobStore); ok {
			data, err := bounded.GetBounded(ctx, digest, limit)
			if errors.Is(err, store.ErrBlobTooLarge) {
				err = fmt.Errorf("%w: %w", errPayloadReadLimit, err)
			}
			return data, err
		}
	}
	// Keep legacy stores and unlimited/unprobeable ceilings compatible.
	// Callers retain their existing post-read admission checks.
	return s.blobs.Get(ctx, digest)
}
