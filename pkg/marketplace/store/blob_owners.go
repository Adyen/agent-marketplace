package store

import (
	"context"
	"errors"
)

var (
	ErrBlobOwnersIncomplete = errors.New("marketplace/store: blob ownership coverage incomplete")
	ErrBlobOwnershipInvalid = errors.New("marketplace/store: invalid blob ownership source")
)

const (
	BlobOwnerReadLimit     = 512
	BlobOwnerBackfillLimit = 32
	BlobOwnerManifestLimit = 4 << 20
)

type BlobOwnerStore interface {
	ListBlobOwners(context.Context, MarketplaceID, string, ArchiveDigest) ([]AssessedResourceRevision, []RegistryID, error)
}

type BlobOwnerMaintenanceStore interface {
	RecordPublicationBlobOwners(context.Context, RegistryRevisionPublication, []BlobUpload) error
	BackfillBlobOwners(context.Context, BlobStore, int) (BlobOwnerBackfillResult, error)
}

type BlobOwnerBackfillResult struct {
	Processed int
	Remaining bool
}

func recordPublicationBlobOwners(ctx context.Context, meta MetadataStore, req RegistryRevisionPublication, uploads []BlobUpload) error {
	maintenance, ok := meta.(BlobOwnerMaintenanceStore)
	if !ok {
		if req.ManagedFence != nil {
			return ErrTopology
		}
		return nil
	}
	return maintenance.RecordPublicationBlobOwners(ctx, req, uploads)
}

func BackfillBlobOwners(ctx context.Context, meta MetadataStore, blobs BlobStore) (BlobOwnerBackfillResult, error) {
	maintenance, ok := meta.(BlobOwnerMaintenanceStore)
	if !ok {
		return BlobOwnerBackfillResult{}, ErrTopology
	}
	return maintenance.BackfillBlobOwners(ctx, blobs, BlobOwnerBackfillLimit)
}
