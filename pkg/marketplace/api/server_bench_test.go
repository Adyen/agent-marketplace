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

func BenchmarkServerSearch(b *testing.B) {
	ctx := context.Background()

	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = meta.Close() }()

	blobs := fsblob.New(b.TempDir())
	cfg := config.Config{
		Instance:         config.Instance{ID: "bench-mp", Name: "Bench", CanonicalURL: "https://bench"},
		Listen:           "127.0.0.1:0",
		OperationsListen: "127.0.0.1:0",
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: store.MarketplaceID(cfg.Instance.ID), Name: cfg.Instance.Name, CanonicalURL: cfg.Instance.CanonicalURL}); err != nil {
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
	if err := meta.PutSource(ctx, src); err != nil {
		b.Fatalf("put source: %v", err)
	}

	const num = 100
	for i := 0; i < num; i++ {
		rev := store.ResourceRevision{
			Coordinate: store.ResourceCoordinate{
				Marketplace: store.MarketplaceID(cfg.Instance.ID),
				Source:      src.ID,
				Kind:        KindSkill,
				Name:        fmt.Sprintf("skill-%04d", i),
			},
			ContentDigest: store.RevisionDigest(fmt.Sprintf("sha256:%064d", i)),
		}
		if err := meta.PutResourceRevision(ctx, rev); err != nil {
			b.Fatalf("put resource revision: %v", err)
		}
	}

	server := NewServer(cfg, meta, blobs)
	srv := httptest.NewServer(server.PublicHandler())
	defer srv.Close()

	client := srv.Client()
	url := srv.URL + "/api/v1/search?q=skill-0050"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(url)
		if err != nil {
			b.Fatalf("get: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("status %d", resp.StatusCode)
		}
	}
}
