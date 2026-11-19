package postgres

import (
	"context"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var _ store.ClaudeDeliveryStateStore = (*Store)(nil)
var _ store.ClaudeDeliveryJobStore = (*Store)(nil)

func (s *Store) LeaseClaudeDeliveryJob(ctx context.Context, marketplace store.MarketplaceID, requestedID store.JobID, workerID string, leaseDuration time.Duration) (store.Job, error) {
	var leased store.Job
	err := s.withTx(ctx, func(tx *Store) error {
		var locked any
		if err := tx.q.QueryRowContext(ctx,
			"SELECT pg_advisory_xact_lock(hashtext($1)::bigint)",
			"agent-marketplace:claude-delivery:"+marketplace.String(),
		).Scan(&locked); err != nil {
			return err
		}
		id, err := store.SelectClaudeDeliveryJob(ctx, tx.q, marketplace, requestedID, time.Now().UTC())
		if err != nil {
			return err
		}
		leased, err = tx.leaseJobByID(ctx, id, workerID, leaseDuration)
		return err
	})
	return leased, err
}

func (s *Store) UpsertClaudeDeliveryPublished(ctx context.Context, row store.ClaudeDeliveryPlugin) error {
	return store.UpsertClaudeDeliveryPublished(ctx, s.q, row)
}

func (s *Store) MarkClaudeDeliveryRetired(ctx context.Context, coordinate store.ResourceCoordinate, at time.Time) error {
	return store.MarkClaudeDeliveryRetired(ctx, s.q, coordinate, at)
}

func (s *Store) MarkClaudeDeliverySkipped(ctx context.Context, coordinate store.ResourceCoordinate, name, code string, at time.Time) error {
	return store.MarkClaudeDeliverySkipped(ctx, s.q, coordinate, name, code, at)
}

func (s *Store) MarkClaudeDeliveryConflict(ctx context.Context, coordinate store.ResourceCoordinate, name, code string, at time.Time) error {
	return store.MarkClaudeDeliveryConflict(ctx, s.q, coordinate, name, code, at)
}

func (s *Store) RecordClaudeDeliveryFailure(ctx context.Context, coordinate store.ResourceCoordinate, name, marketplaceID, code string, at time.Time) error {
	return store.RecordClaudeDeliveryFailure(ctx, s.q, coordinate, name, marketplaceID, code, at)
}

func (s *Store) GetClaudeDeliveryPlugin(ctx context.Context, coordinate store.ResourceCoordinate) (store.ClaudeDeliveryPlugin, error) {
	return store.GetClaudeDeliveryPlugin(ctx, s.q, coordinate)
}

func (s *Store) ListClaudeDeliveryPlugins(ctx context.Context) ([]store.ClaudeDeliveryPlugin, error) {
	return store.ListClaudeDeliveryPlugins(ctx, s.q)
}

func (s *Store) ListClaudeDeliveryDesired(ctx context.Context, publications store.PublicationStore, marketplace store.MarketplaceID, registries []store.RegistryID) ([]store.ClaudeDeliveryDesired, error) {
	return store.ReadClaudeDeliveryDesired(ctx, s, publications, marketplace, registries)
}
