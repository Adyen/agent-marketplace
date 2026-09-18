package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func BenchmarkManagedRefreshReadsSQLite(b *testing.B) {
	ctx := context.Background()
	meta := sqlite.New(filepath.Join(b.TempDir(), "runtime.db"))
	if err := meta.Open(ctx); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Benchmark", CanonicalURL: "https://marketplace.test"}); err != nil {
		b.Fatal(err)
	}
	if _, err := meta.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "benchmark")); err != nil {
		b.Fatal(err)
	}
	job, fence := enqueueRuntimeRefresh(b, meta, "benchmark-refresh", 1, 1, 1, "", 1)
	if err := meta.RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
		b.Fatal(err)
	}
	b.Run("events", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			events, err := meta.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 20})
			if err != nil || len(events) != 1 {
				b.Fatalf("events=%d: %v", len(events), err)
			}
		}
	})
	b.Run("job", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := meta.GetManagedRefreshJob(ctx, "managed-test", job.ID); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("registry-state", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := meta.GetManagedRegistryRefreshState(ctx, "managed-test", "registry"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCurrentManagedConfigurationSQLite(b *testing.B) {
	ctx := context.Background()
	meta := sqlite.New(filepath.Join(b.TempDir(), "metadata.db"))
	if err := meta.Open(ctx); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Benchmark", CanonicalURL: "https://marketplace.test"}); err != nil {
		b.Fatal(err)
	}
	if _, err := meta.ApplyManagedConfiguration(ctx, managedRequest(0, "benchmark")); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := meta.CurrentManagedConfiguration(ctx, "managed-test"); err != nil {
			b.Fatal(err)
		}
	}
}
