package postgres

import (
	"context"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s *Store) ResetRegistryCatalog(ctx context.Context, id store.RegistryID, dryRun bool) (map[string]int64, error) {
	var counts map[string]int64
	err := s.withTx(ctx, func(tx *Store) error {
		var err error
		counts, err = store.ResetRegistryCatalogSQL(ctx, tx.q, id, dryRun)
		return err
	})
	if err != nil {
		return nil, err
	}
	return counts, nil
}
