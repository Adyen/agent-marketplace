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

	registry := store.Registry{
		ID:          store.RegistryID("bench-registry"),
		Slug:        "bench",
		Ref:         "main",
		Origins:     []string{"https://bench.git"},
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := s.PutRegistry(ctx, registry); err != nil {
		b.Fatalf("put registry: %v", err)
	}

	const commit = "bench-commit"
	manifest := store.RevisionDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: registry.ID, Ref: registry.Ref, Commit: commit, RegistryRevisionManifestDigest: manifest,
		Acquisition: registry.Acquisition, OccurredAt: time.Now().UTC(),
	}); err != nil {
		b.Fatalf("put registry occurrence: %v", err)
	}

	const num = 100
	for i := 0; i < num; i++ {
		rev := store.ResourceRevision{
			Coordinate: store.ResourceCoordinate{
				Marketplace: mpID,
				Registry:    registry.ID,
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

// BenchmarkCatalogProjectionPage measures a continuation page with direct
// collection references after setup has published 100 current resources.
func BenchmarkCatalogProjectionPage(b *testing.B) {
	ctx := context.Background()
	s := New(":memory:")
	if err := s.Open(ctx); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutMarketplace(ctx, store.Marketplace{ID: "projection", Name: "Projection", CanonicalURL: "https://projection.test"}); err != nil {
		b.Fatal(err)
	}
	registry := store.Registry{ID: "projection-registry", Slug: "projection", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}
	if err := s.PutRegistry(ctx, registry); err != nil {
		b.Fatal(err)
	}
	manifest := store.RevisionDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: registry.ID, Ref: registry.Ref, Commit: "projection-commit", RegistryRevisionManifestDigest: manifest, Acquisition: registry.Acquisition}); err != nil {
		b.Fatal(err)
	}
	putCurrent := func(kind, name string, digestIndex int) store.ResourceRevision {
		rev := store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "projection", Registry: registry.ID, Kind: kind, Name: name},
			ContentDigest: store.RevisionDigest(fmt.Sprintf("sha256:%064d", digestIndex)),
		}
		if err := s.PutResourceRevision(ctx, rev); err != nil {
			b.Fatal(err)
		}
		if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Commit: "projection-commit", ManifestDigest: manifest}); err != nil {
			b.Fatal(err)
		}
		assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, AnalyzerVersion: "benchmark/v1", PolicyDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", AssessedAt: time.Unix(1, 0).UTC()})
		if err != nil {
			b.Fatal(err)
		}
		if err := s.PutResourceAssessment(ctx, assessment); err != nil {
			b.Fatal(err)
		}
		if err := s.SetCurrentResourceAssessment(ctx, assessment); err != nil {
			b.Fatal(err)
		}
		return rev
	}
	for i := 0; i < 100; i++ {
		putCurrent("skill", fmt.Sprintf("skill-%04d", i), i)
	}
	for i := 0; i < 50; i++ {
		collection := putCurrent("collection", fmt.Sprintf("collection-%04d", i), 100+i)
		if err := s.PutCollectionRevision(ctx, store.CollectionRevision{
			ID:             string(collection.ContentDigest),
			RegistryID:     registry.ID,
			ManifestDigest: collection.ContentDigest,
			Members: []store.ResourceCoordinate{
				{Marketplace: "projection", Registry: registry.ID, Kind: "skill", Name: fmt.Sprintf("skill-%04d", i*2)},
				{Marketplace: "projection", Registry: registry.ID, Kind: "skill", Name: fmt.Sprintf("skill-%04d", i*2+1)},
			},
			Includes: []store.ResourceCoordinate{{
				Marketplace: "projection", Registry: registry.ID, Kind: "collection", Name: fmt.Sprintf("collection-%04d", (i+1)%50),
			}},
		}); err != nil {
			b.Fatal(err)
		}
	}
	first, err := s.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "projection", Limit: 25})
	if err != nil {
		b.Fatal(err)
	}
	request := store.CatalogProjectionPageRequest{Marketplace: "projection", After: first.Next, Limit: 25}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ReadCatalogProjectionPage(ctx, request); err != nil {
			b.Fatal(err)
		}
	}
}
