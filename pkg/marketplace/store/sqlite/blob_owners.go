package sqlite

import (
	"context"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var _ store.BlobOwnerStore = (*Store)(nil)
var _ store.BlobOwnerMaintenanceStore = (*Store)(nil)

func (s *Store) ListBlobOwners(ctx context.Context, marketplace store.MarketplaceID, kind string, digest store.ArchiveDigest) ([]store.AssessedResourceRevision, []store.RegistryID, error) {
	var revisions []store.AssessedResourceRevision
	var registries []store.RegistryID
	err := s.withTx(ctx, func(tx *Store) error {
		var err error
		revisions, registries, err = store.ReadBlobOwners(ctx, tx.q, tx, marketplace, kind, digest)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return revisions, registries, nil
}

func (s *Store) RecordPublicationBlobOwners(ctx context.Context, req store.RegistryRevisionPublication, uploads []store.BlobUpload) error {
	return s.withTx(ctx, func(tx *Store) error { return store.RecordPublicationBlobOwnership(ctx, tx.q, tx, req, uploads) })
}

func (s *Store) BackfillBlobOwners(ctx context.Context, blobs store.BlobStore, limit int) (store.BlobOwnerBackfillResult, error) {
	var result store.BlobOwnerBackfillResult
	err := s.withTx(ctx, func(tx *Store) error {
		var err error
		result, err = store.BackfillBlobOwnership(ctx, tx.q, tx, blobs, limit)
		return err
	})
	if err != nil {
		return store.BlobOwnerBackfillResult{}, err
	}
	return result, nil
}
