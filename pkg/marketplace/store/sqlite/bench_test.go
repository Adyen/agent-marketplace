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

	const commit = "bench-commit"
	manifest := store.RevisionDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := s.PutSourceOccurrence(ctx, store.SourceOccurrence{
		SourceID: src.ID, Ref: src.Ref, Commit: commit, SourceRevisionManifestDigest: manifest,
		Acquisition: src.Acquisition, OccurredAt: time.Now().UTC(),
	}); err != nil {
		b.Fatalf("put source occurrence: %v", err)
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
			ContentDigest: store.RevisionDigest(fmt.Sprintf("sha256:%064d", i)),
		}
		if err := s.PutResourceRevision(ctx, rev); err != nil {
			b.Fatalf("put resource revision: %v", err)
		}
		if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
			Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Commit: commit, ManifestDigest: manifest,
		}); err != nil {
			b.Fatalf("put resource occurrence: %v", err)
		}
		assessment := store.ResourceAssessment{
			Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, AnalyzerVersion: "1.0.0",
			PolicyDigest:             "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Decision:                 store.AssessmentDecisionAllow,
			InspectionManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			AssessedAt:               time.Now().UTC(),
		}
		if err := s.PutResourceAssessment(ctx, assessment); err != nil {
			b.Fatalf("put resource assessment: %v", err)
		}
		if err := s.SetCurrentResourceAssessment(ctx, assessment); err != nil {
			b.Fatalf("set current resource assessment: %v", err)
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
