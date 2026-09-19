package sqlite

import (
	"context"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var _ store.ManagedPreviewStore = (*Store)(nil)

func (s *Store) CreateManagedPreview(ctx context.Context, preview store.ManagedPreview, job store.Job) (store.ManagedPreview, error) {
	var created store.ManagedPreview
	err := s.withTx(ctx, func(tx *Store) error {
		var err error
		created, err = store.InsertManagedPreview(ctx, tx.q, tx, preview, job)
		return err
	})
	return created, err
}

func (s *Store) GetManagedPreview(ctx context.Context, marketplace store.MarketplaceID, id string) (store.ManagedPreview, error) {
	return store.ReadManagedPreview(ctx, s.q, marketplace, id)
}

func (s *Store) UpdateManagedPreview(ctx context.Context, preview store.ManagedPreview) error {
	return s.withTx(ctx, func(tx *Store) error { return store.ReplaceManagedPreview(ctx, tx.q, preview) })
}

func (s *Store) PruneManagedPreviews(ctx context.Context, marketplace store.MarketplaceID, before time.Time) error {
	return store.DeleteManagedPreviewsBefore(ctx, s.q, marketplace, before)
}
