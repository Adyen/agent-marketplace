package sqlite

import (
	"context"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var _ store.PublicationStore = (*Store)(nil)

func (s *Store) PublishRegistryPublication(ctx context.Context, req store.RegistryRevisionPublication) error {
	return s.withTx(ctx, func(tx *Store) error { return store.CommitRegistryPublication(ctx, tx.q, tx, req) })
}
func (s *Store) GetRegistryPublication(ctx context.Context, registry store.RegistryID, id store.PublicationID) (store.RegistryPublication, error) {
	return store.ReadRegistryPublication(ctx, s.q, registry, id)
}
func (s *Store) CurrentRegistryPublication(ctx context.Context, registry store.RegistryID) (store.RegistryPublication, error) {
	return store.ReadRegistryPublication(ctx, s.q, registry, "")
}
