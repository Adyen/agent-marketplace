package sqlite

import (
	"context"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var _ store.ManagedConfigurationStore = (*Store)(nil)

func (s *Store) FindManagedChange(ctx context.Context, id store.MarketplaceID, actor store.ManagedActor, operation, key string) (store.ManagedChange, error) {
	return store.ReadManagedChange(ctx, s.q, id, actor, operation, key)
}

func (s *Store) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	return store.ReadManagedConfiguration(ctx, s.q, id)
}

func (s *Store) ManagedConfigurationHistory(ctx context.Context, id store.MarketplaceID, before int64, limit int) ([]store.ManagedChange, error) {
	return store.ReadManagedConfigurationHistory(ctx, s.q, id, before, limit)
}

func (s *Store) ApplyManagedConfiguration(ctx context.Context, req store.ManagedChangeRequest) (store.ManagedChange, error) {
	var accepted store.ManagedChange
	err := s.withTx(ctx, func(tx *Store) error {
		var err error
		accepted, err = store.CommitManagedConfiguration(ctx, tx.q, tx, req)
		return err
	})
	if err != nil {
		return store.ManagedChange{}, err
	}
	return accepted, nil
}
