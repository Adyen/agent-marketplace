package postgres

import (
	"context"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var _ store.ResourceArtifactStore = (*Store)(nil)

func (s *Store) ArtifactCoverage(ctx context.Context, marketplace store.MarketplaceID) (store.ArtifactCoverage, error) {
	return store.ReadArtifactCoverage(ctx, s.q, marketplace)
}

func (s *Store) PutResourceArtifact(ctx context.Context, a store.ResourceArtifact) error {
	return s.withTx(ctx, func(tx *Store) error { return store.WriteResourceArtifact(ctx, tx.q, a) })
}
func (s *Store) ListResourceArtifacts(ctx context.Context, c store.ResourceCoordinate, d store.RevisionDigest) ([]store.ResourceArtifact, error) {
	return store.ReadResourceArtifacts(ctx, s.q, c, d)
}
func (s *Store) ListResourceArtifactsByBlob(ctx context.Context, m store.MarketplaceID, d artifact.BlobDigest) ([]store.ResourceArtifact, error) {
	return store.ReadResourceArtifactsByBlob(ctx, s.q, m, d)
}
