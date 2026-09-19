package admin

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s Service) AdoptMaintenance(ctx context.Context, cfg config.Config, blobs store.BlobStore, req AdoptionRequest) (store.ManagedChange, error) {
	for {
		if err := ctx.Err(); err != nil {
			return store.ManagedChange{}, err
		}
		result, err := store.BackfillBlobOwners(ctx, s.Metadata, blobs)
		if err != nil {
			return store.ManagedChange{}, fmt.Errorf("prepare adoption ownership coverage: %w", err)
		}
		slog.InfoContext(ctx, "marketplace adoption ownership backfill", "processed", result.Processed, "remaining", result.Remaining)
		if !result.Remaining {
			break
		}
		if result.Processed == 0 {
			return store.ManagedChange{}, store.ErrBlobOwnersIncomplete
		}
	}
	return s.Adopt(ctx, cfg, req)
}
