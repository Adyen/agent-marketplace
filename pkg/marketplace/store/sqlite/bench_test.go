package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkSearchResourceRevisions(b *testing.B) {
	ctx := context.Background()
	s := New(":memory:")
	if err := s.Open(ctx); err != nil {
		b.Fatalf("open: %v", err)
	}
	defer func() { _ = s.Close() }()

	mpID := store.MarketplaceID("bench-mp")
	if err := s.PutMarketplace(ctx, store.Marketplace{ID: mpID, Name: "Bench", CanonicalURL: "https://bench"}); err != nil {
		b.Fatalf("put marketplace: %v", err)
	}

	src := store.Source{
		ID:          store.SourceID("bench-source"),
		Slug:        "bench",
		Ref:         "main",
		Origins:     []string{"https://bench.git"},
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := s.PutSource(ctx, src); err != nil {
		b.Fatalf("put source: %v", err)
	}

	const num = 100
	for i := 0; i < num; i++ {
		rev := store.ResourceRevision{
			Coordinate: store.ResourceCoordinate{
				Marketplace: mpID,
				Source:      src.ID,
				Kind:        "skill",
				Name:        fmt.Sprintf("skill-%04d", i),
			},
			ContentDigest:   store.RevisionDigest(fmt.Sprintf("sha256:%064d", i)),
			AnalyzerVersion: "1.0.0",
		}
		if err := s.PutResourceRevision(ctx, rev); err != nil {
			b.Fatalf("put resource revision: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.SearchResourceRevisions(ctx, store.ResourceSearchFilter{
			Marketplace: mpID,
			Query:       "skill-0050",
		})
		if err != nil {
			b.Fatalf("search: %v", err)
		}
	}
}
