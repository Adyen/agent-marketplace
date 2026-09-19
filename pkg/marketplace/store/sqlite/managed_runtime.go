package sqlite

import (
	"context"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var _ store.ManagedRefreshAttemptStore = (*Store)(nil)

func (s *Store) RecordManagedRefreshAttempt(ctx context.Context, fence store.ManagedFence, outcome string, publication store.PublicationID) error {
	return s.withTx(ctx, func(tx *Store) error {
		return store.CommitManagedRefreshAttempt(ctx, tx.q, tx, fence, outcome, publication)
	})
}

func (s *Store) ListManagedRefreshAttempts(ctx context.Context, query store.ManagedRefreshAttemptQuery) ([]store.ManagedRefreshAttempt, error) {
	return store.ReadManagedRefreshAttempts(ctx, s.q, query)
}

func (s *Store) GetManagedRegistryRefreshState(ctx context.Context, marketplace store.MarketplaceID, registry store.RegistryID) (store.ManagedRegistryRefreshState, error) {
	var result store.ManagedRegistryRefreshState
	err := s.withTx(ctx, func(tx *Store) error {
		var err error
		result, err = store.ReadManagedRegistryRefreshState(ctx, tx.q, tx, marketplace, registry)
		return err
	})
	if err != nil {
		return store.ManagedRegistryRefreshState{}, err
	}
	return result, nil
}

func (s *Store) GetManagedRefreshJob(ctx context.Context, marketplace store.MarketplaceID, id store.JobID) (store.ManagedRefreshJobView, error) {
	return store.ReadManagedRefreshJob(ctx, s, marketplace, id)
}
