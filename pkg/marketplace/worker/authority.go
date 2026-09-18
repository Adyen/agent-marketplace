package worker

import (
	"context"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func checkAuthority(ctx context.Context, deps Deps) error {
	err := store.CheckConfigurationAuthority(ctx, deps.MetadataStore, store.MarketplaceID(deps.Config.Instance.ID), deps.Config.Management.Mode)
	if err != nil {
		slog.ErrorContext(ctx, "worker configuration authority rejected", "marketplace", deps.Config.Instance.ID)
	}
	return err
}
