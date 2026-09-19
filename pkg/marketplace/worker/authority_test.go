package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type adoptedMetadata struct {
	store.MetadataStore
	store.ManagedConfigurationStore
}

func (adoptedMetadata) CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error) {
	return store.ManagedChange{Revision: 1}, nil
}

// Test decorators must preserve the underlying database's authority reader.
func (s *externallyCompletingStore) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	return s.MetadataStore.(store.ManagedConfigurationStore).CurrentManagedConfiguration(ctx, id)
}
func (s *failingRevocationStore) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	return s.MetadataStore.(store.ManagedConfigurationStore).CurrentManagedConfiguration(ctx, id)
}
func (s *slowUnrelatedRegistryStore) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	return s.MetadataStore.(store.ManagedConfigurationStore).CurrentManagedConfiguration(ctx, id)
}

func TestLegacyWorkerRejectsAdoptedDatabaseBeforeWrites(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()
	cfg := config.Config{Instance: config.Instance{ID: "market", Name: "stale YAML"}, Registries: []config.Registry{{ID: "reg", Slug: "stale", Origin: "https://example.com/repo", Ref: "stale"}}}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "market", Name: "current"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistry(ctx, store.Registry{ID: "reg", Slug: "current", Ref: "current", Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	deps := Deps{Config: cfg, MetadataStore: adoptedMetadata{MetadataStore: meta}, BlobStore: blobs}
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"ensure marketplace", func() error { return EnsureMarketplace(ctx, &deps) }},
		{"ensure revocations", func() error { return EnsureRevocations(ctx, deps) }},
		{"refresh", func() error { return Refresh(ctx, deps, store.Registry{ID: "reg"}) }},
		{"reindex", func() error { return Reindex(ctx, deps, cfg, "reg") }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); !errors.Is(err, store.ErrAuthorityModeConflict) {
				t.Fatalf("got %v", err)
			}
			registry, err := meta.GetRegistry(ctx, "reg")
			if err != nil || registry.Ref != "current" || registry.Slug != "current" {
				t.Fatalf("registry changed: %+v, %v", registry, err)
			}
			market, err := meta.GetMarketplace(ctx, "market")
			if err != nil || market.Name != "current" {
				t.Fatalf("market changed: %+v, %v", market, err)
			}
		})
	}
}
