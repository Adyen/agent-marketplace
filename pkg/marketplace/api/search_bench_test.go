package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

// benchSearchServer builds a server backed by an in-memory SQLite store holding
// num listed skill revisions. It is shared by the search benchmarks so they
// measure the same corpus.
func benchSearchServer(b *testing.B, num int) *Server {
	b.Helper()
	ctx := context.Background()

	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	b.Cleanup(func() { _ = meta.Close() })

	cfg := config.Config{
		Instance:         config.Instance{ID: "bench-mp", Name: "Bench", CanonicalURL: "https://bench"},
		Listen:           "127.0.0.1:0",
		OperationsListen: "127.0.0.1:0",
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: store.MarketplaceID(cfg.Instance.ID), Name: cfg.Instance.Name, CanonicalURL: cfg.Instance.CanonicalURL}); err != nil {
		b.Fatalf("put marketplace: %v", err)
	}

	src := store.Registry{
		ID:          store.RegistryID("bench-source"),
		Slug:        "bench",
		Ref:         "main",
		Origins:     []string{"https://bench.git"},
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := meta.PutRegistry(ctx, src); err != nil {
		b.Fatalf("put source: %v", err)
	}

	for i := 0; i < num; i++ {
		if err := meta.PutResourceRevision(ctx, benchRevision(src.ID, store.MarketplaceID(cfg.Instance.ID), i)); err != nil {
			b.Fatalf("put resource revision: %v", err)
		}
	}

	return NewServer(cfg, meta, fsblob.New(b.TempDir()))
}

// benchDescription is a realistic one-line summary, sized like the
// descriptions the production instance actually serves.
const benchDescription = "Analyze user retention and engagement patterns by cohort. " +
	"Upload your data or describe what you need, and get retention curves, " +
	"feature adoption trends, and actionable insights."

func benchRevision(source store.RegistryID, marketplace store.MarketplaceID, i int) store.ResourceRevision {
	return store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: marketplace,
			Registry:    source,
			Kind:        KindSkill,
			Name:        fmt.Sprintf("skill-%04d", i),
		},
		ContentDigest: store.RevisionDigest(fmt.Sprintf("sha256:%064d", i)),
		ArchiveDigest: store.ArchiveDigest(fmt.Sprintf("sha256:%064d", i)),
		License:       "MIT",
		Description:   benchDescription,
	}
}

// BenchmarkSearchHandler measures the search hot path without a loopback TCP
// connection, so the number reflects store access, revocation filtering,
// sorting, and DTO construction rather than the host's socket latency.
func BenchmarkSearchHandler(b *testing.B) {
	handler := benchSearchServer(b, 100).PublicHandler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=skill-0050", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			b.Fatalf("status %d", recorder.Code)
		}
	}
}

// BenchmarkSearchHandlerAll measures the unfiltered listing, which is what the
// index page requests and therefore the widest DTO conversion fan-out.
func BenchmarkSearchHandlerAll(b *testing.B) {
	handler := benchSearchServer(b, 100).PublicHandler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			b.Fatalf("status %d", recorder.Code)
		}
	}
}
