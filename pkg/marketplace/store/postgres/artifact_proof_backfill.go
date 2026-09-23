package postgres

import (
	"context"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var _ store.ArtifactProofBackfillStore = (*Store)(nil)

func (s *Store) ListArtifactArchiveDigests(ctx context.Context, marketplace store.MarketplaceID, after store.ArchiveDigest, limit int) ([]store.ArchiveDigest, error) {
	return store.ListArtifactArchiveDigestsSQL(ctx, s.q, marketplace, after, limit)
}
