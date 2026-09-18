package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func BenchmarkPublicationProjection(b *testing.B) {
	ctx := context.Background()
	meta := sqlite.New(filepath.Join(b.TempDir(), "catalog.db"))
	if err := meta.Open(ctx); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Benchmark", CanonicalURL: "https://example.test"}); err != nil {
		b.Fatal(err)
	}
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Listed: true, Ref: "main", Acquisition: store.AcquisitionIndexOnly}); err != nil {
		b.Fatal(err)
	}
	names := make([]string, 100)
	for i := range names {
		names[i] = fmt.Sprintf("skill-%03d", i)
	}
	for i := range 5 {
		if err := meta.PublishRegistryPublication(ctx, publicationRequest(b, store.PublicationID(fmt.Sprintf("pub-%d", i)), names...)); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		page, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 100})
		if err != nil || len(page.Resources) != 100 {
			b.Fatalf("projection length=%d: %v", len(page.Resources), err)
		}
	}
}
