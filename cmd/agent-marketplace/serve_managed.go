package main

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func reindexTargets(ctx context.Context, meta store.MetadataStore, cfg mpconfig.Config, name string) ([]string, error) {
	var targets []string
	if cfg.Management.Mode == "database" {
		view, err := (admin.Service{Metadata: meta}).Read(ctx, store.MarketplaceID(cfg.Instance.ID))
		if err != nil {
			return nil, err
		}
		for _, registry := range view.Snapshot.Registries {
			if name != "" && (name == string(registry.ID) || name == registry.Slug) {
				return []string{string(registry.ID)}, nil
			}
			if name == "" && registry.Active() {
				targets = append(targets, string(registry.ID))
			}
		}
	} else {
		for _, registry := range cfg.Registries {
			if name != "" && (name == registry.ID || name == registry.Slug) {
				return []string{registry.ID}, nil
			}
			if name == "" {
				targets = append(targets, registry.ID)
			}
		}
	}
	if name != "" {
		return nil, fmt.Errorf("registry %q: %w", name, store.ErrNotFound)
	}
	return targets, nil
}
